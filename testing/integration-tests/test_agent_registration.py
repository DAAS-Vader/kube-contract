"""
DaaS Agent Registration Integration Tests

Tests the complete agent registration flow including:
- Seal token generation and validation
- Sui blockchain stake verification
- K3s cluster joining
- Node status verification
"""

import pytest
import asyncio
import time
import requests
from kubernetes import client, config
from kubernetes.client.rest import ApiException

from .utils.daas_client import DaaSClient
from .utils.k3s_client import K3sClient
from .utils.sui_client import SuiTestClient
from .utils.test_helpers import wait_for_condition, retry_async


class TestAgentRegistration:
    """Test suite for DaaS agent registration process."""

    @pytest.fixture(autouse=True)
    async def setup_test_environment(self):
        """Set up test environment before each test."""
        self.daas_client = DaaSClient()
        self.k3s_client = K3sClient()
        self.sui_client = SuiTestClient()

        # Ensure services are ready
        await self._wait_for_services_ready()

        yield

        # Cleanup after each test
        await self._cleanup_test_nodes()

    async def _wait_for_services_ready(self):
        """Wait for all required services to be ready."""
        services = [
            ("Sui Node", "http://sui-node:9000/health"),
            ("Walrus Simulator", "http://walrus-simulator:31415/v1/health"),
            ("K3s Server", "https://k3s-server:6443/healthz"),
            ("Nautilus Attestation", "http://nautilus-attestation:8090/health")
        ]

        for service_name, health_url in services:
            await wait_for_condition(
                lambda: self._check_service_health(health_url),
                timeout=120,
                interval=5,
                description=f"Waiting for {service_name} to be ready"
            )

    def _check_service_health(self, url: str) -> bool:
        """Check if a service is healthy."""
        try:
            response = requests.get(url, timeout=5, verify=False)
            return response.status_code == 200
        except Exception:
            return False

    async def _cleanup_test_nodes(self):
        """Clean up test nodes after each test."""
        try:
            nodes = await self.k3s_client.list_nodes()
            for node in nodes:
                if node.metadata.name.startswith("daas-test-"):
                    await self.k3s_client.delete_node(node.metadata.name)
        except Exception as e:
            print(f"Warning: Failed to cleanup test nodes: {e}")

    @pytest.mark.asyncio
    async def test_successful_agent_registration(self):
        """Test successful agent registration with valid stake and signature."""

        # Step 1: Generate test wallet and stake
        wallet_address = "0x1234567890abcdef1234567890abcdef12345678"
        stake_amount = 2000000000  # 2 SUI (above minimum)

        # Simulate stake setup on Sui
        await self.sui_client.setup_test_stake(wallet_address, stake_amount)

        # Step 2: Generate Seal token
        seal_token = await self.daas_client.generate_seal_token(
            wallet_address=wallet_address,
            challenge="test-challenge-001"
        )

        assert seal_token.startswith("SEAL")
        assert wallet_address in seal_token

        # Step 3: Start agent with DaaS authentication
        agent_config = {
            "server_url": "https://k3s-server:6443",
            "token": seal_token,
            "node_name": "daas-test-worker-001",
            "daas_enabled": True,
            "sui_rpc_endpoint": "http://sui-node:9000",
            "walrus_endpoint": "http://walrus-simulator:31415"
        }

        # Simulate agent registration process
        registration_result = await self.daas_client.register_agent(agent_config)

        assert registration_result["status"] == "success"
        assert registration_result["node_name"] == "daas-test-worker-001"
        assert registration_result["authentication_method"] == "daas-seal"

        # Step 4: Verify node appears in cluster
        await wait_for_condition(
            lambda: self._check_node_registered("daas-test-worker-001"),
            timeout=60,
            interval=2,
            description="Waiting for node to appear in cluster"
        )

        # Step 5: Verify node is ready
        node = await self.k3s_client.get_node("daas-test-worker-001")

        assert node is not None
        assert "daas.io/blockchain-enabled" in node.metadata.labels
        assert "daas.io/stake-validated" in node.metadata.labels
        assert node.metadata.labels["daas.io/wallet-address"] == wallet_address

        # Verify node conditions
        ready_condition = next(
            (c for c in node.status.conditions if c.type == "Ready"),
            None
        )
        assert ready_condition is not None
        assert ready_condition.status == "True"

    async def _check_node_registered(self, node_name: str) -> bool:
        """Check if a node is registered in the cluster."""
        try:
            node = await self.k3s_client.get_node(node_name)
            return node is not None
        except ApiException:
            return False

    @pytest.mark.asyncio
    async def test_registration_failure_insufficient_stake(self):
        """Test registration failure when wallet has insufficient stake."""

        wallet_address = "0xabcdef1234567890abcdef1234567890abcdef12"
        insufficient_stake = 500000000  # 0.5 SUI (below minimum)

        # Setup wallet with insufficient stake
        await self.sui_client.setup_test_stake(wallet_address, insufficient_stake)

        # Generate Seal token
        seal_token = await self.daas_client.generate_seal_token(
            wallet_address=wallet_address,
            challenge="test-challenge-002"
        )

        # Attempt agent registration
        agent_config = {
            "server_url": "https://k3s-server:6443",
            "token": seal_token,
            "node_name": "daas-test-worker-002",
            "daas_enabled": True
        }

        with pytest.raises(Exception) as exc_info:
            await self.daas_client.register_agent(agent_config)

        assert "insufficient stake" in str(exc_info.value).lower()

        # Verify node is NOT in cluster
        await asyncio.sleep(5)  # Give time for any erroneous registration

        with pytest.raises(ApiException):
            await self.k3s_client.get_node("daas-test-worker-002")

    @pytest.mark.asyncio
    async def test_registration_failure_invalid_signature(self):
        """Test registration failure with invalid Seal signature."""

        wallet_address = "0x2468135790abcdef2468135790abcdef24681357"
        stake_amount = 2000000000  # Sufficient stake

        # Setup valid stake
        await self.sui_client.setup_test_stake(wallet_address, stake_amount)

        # Create invalid Seal token (wrong signature)
        invalid_seal_token = f"SEAL{wallet_address}::invalid-signature::test-challenge-003"

        # Attempt agent registration
        agent_config = {
            "server_url": "https://k3s-server:6443",
            "token": invalid_seal_token,
            "node_name": "daas-test-worker-003",
            "daas_enabled": True
        }

        with pytest.raises(Exception) as exc_info:
            await self.daas_client.register_agent(agent_config)

        assert "signature validation failed" in str(exc_info.value).lower()

    @pytest.mark.asyncio
    async def test_fallback_to_traditional_authentication(self):
        """Test fallback to traditional K3s authentication when DaaS fails."""

        # Use traditional K3s token
        traditional_token = "daas-test-token-12345"

        agent_config = {
            "server_url": "https://k3s-server:6443",
            "token": traditional_token,
            "node_name": "daas-test-worker-004",
            "daas_enabled": False  # Explicitly disable DaaS
        }

        # Register agent with traditional auth
        registration_result = await self.daas_client.register_agent(agent_config)

        assert registration_result["status"] == "success"
        assert registration_result["authentication_method"] == "traditional"

        # Verify node is registered
        await wait_for_condition(
            lambda: self._check_node_registered("daas-test-worker-004"),
            timeout=60,
            interval=2
        )

        node = await self.k3s_client.get_node("daas-test-worker-004")

        # Should NOT have DaaS-specific labels
        assert "daas.io/blockchain-enabled" not in node.metadata.labels
        assert "daas.io/stake-validated" not in node.metadata.labels

    @pytest.mark.asyncio
    async def test_multiple_agent_registration(self):
        """Test registration of multiple agents simultaneously."""

        registration_tasks = []
        wallet_addresses = [
            "0x1111111111111111111111111111111111111111",
            "0x2222222222222222222222222222222222222222",
            "0x3333333333333333333333333333333333333333"
        ]

        # Setup stakes for all wallets
        for i, wallet_address in enumerate(wallet_addresses):
            await self.sui_client.setup_test_stake(wallet_address, 2000000000)

            # Generate Seal token
            seal_token = await self.daas_client.generate_seal_token(
                wallet_address=wallet_address,
                challenge=f"test-challenge-{i+100}"
            )

            # Create registration task
            agent_config = {
                "server_url": "https://k3s-server:6443",
                "token": seal_token,
                "node_name": f"daas-test-worker-{i+100}",
                "daas_enabled": True
            }

            task = self.daas_client.register_agent(agent_config)
            registration_tasks.append(task)

        # Wait for all registrations to complete
        results = await asyncio.gather(*registration_tasks, return_exceptions=True)

        # Verify all registrations succeeded
        for i, result in enumerate(results):
            assert not isinstance(result, Exception), f"Registration {i} failed: {result}"
            assert result["status"] == "success"

        # Verify all nodes are in cluster
        for i in range(3):
            node_name = f"daas-test-worker-{i+100}"
            await wait_for_condition(
                lambda name=node_name: self._check_node_registered(name),
                timeout=60,
                interval=2
            )

    @pytest.mark.asyncio
    async def test_agent_reconnection_after_restart(self):
        """Test agent reconnection after restart maintains DaaS state."""

        wallet_address = "0x5555555555555555555555555555555555555555"
        stake_amount = 2000000000

        # Initial registration
        await self.sui_client.setup_test_stake(wallet_address, stake_amount)

        seal_token = await self.daas_client.generate_seal_token(
            wallet_address=wallet_address,
            challenge="test-challenge-restart"
        )

        agent_config = {
            "server_url": "https://k3s-server:6443",
            "token": seal_token,
            "node_name": "daas-test-worker-restart",
            "daas_enabled": True
        }

        # Initial registration
        result = await self.daas_client.register_agent(agent_config)
        assert result["status"] == "success"

        # Wait for node to be ready
        await wait_for_condition(
            lambda: self._check_node_registered("daas-test-worker-restart"),
            timeout=60,
            interval=2
        )

        # Simulate agent restart
        await self.daas_client.restart_agent("daas-test-worker-restart")

        # Wait for reconnection
        await wait_for_condition(
            lambda: self._check_node_ready("daas-test-worker-restart"),
            timeout=60,
            interval=2,
            description="Waiting for node to be ready after restart"
        )

        # Verify DaaS state is maintained
        node = await self.k3s_client.get_node("daas-test-worker-restart")
        assert "daas.io/blockchain-enabled" in node.metadata.labels
        assert node.metadata.labels["daas.io/wallet-address"] == wallet_address

    async def _check_node_ready(self, node_name: str) -> bool:
        """Check if a node is ready."""
        try:
            node = await self.k3s_client.get_node(node_name)
            if node is None:
                return False

            ready_condition = next(
                (c for c in node.status.conditions if c.type == "Ready"),
                None
            )
            return ready_condition is not None and ready_condition.status == "True"
        except Exception:
            return False