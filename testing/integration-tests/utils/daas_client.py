"""
DaaS Client for integration testing.
Provides high-level interface for DaaS operations.
"""

import asyncio
import hashlib
import json
import time
from typing import Dict, Any, Optional, List
import aiohttp
from .test_helpers import retry_async, TestTimer


class DaaSClient:
    """Client for DaaS system integration testing."""

    def __init__(self, base_url: str = "http://localhost:8080"):
        self.base_url = base_url
        self.session = None
        self._stake_monitors = {}

    async def __aenter__(self):
        self.session = aiohttp.ClientSession()
        return self

    async def __aexit__(self, exc_type, exc_val, exc_tb):
        if self.session:
            await self.session.close()

    async def _ensure_session(self):
        """Ensure HTTP session is available."""
        if not self.session:
            self.session = aiohttp.ClientSession()

    @retry_async(max_attempts=3, delay=1.0)
    async def generate_seal_token(
        self,
        wallet_address: str,
        challenge: str,
        private_key: Optional[str] = None
    ) -> str:
        """
        Generate a Seal authentication token.

        Args:
            wallet_address: Wallet address for the token
            challenge: Challenge string
            private_key: Optional private key (uses test key if not provided)

        Returns:
            Seal token string
        """
        # For testing, use deterministic signature generation
        message = f"{challenge}:{int(time.time())}:{wallet_address}"

        # Simulate signature generation (in production, use actual crypto)
        test_private_key = private_key or "0x" + "1" * 64
        signature_input = test_private_key + message
        signature = hashlib.sha256(signature_input.encode()).hexdigest()

        seal_token = f"SEAL{wallet_address}::{signature}::{challenge}"
        return seal_token

    @retry_async(max_attempts=3, delay=1.0)
    async def validate_stake(
        self,
        wallet_address: str,
        min_stake: int,
        use_cache: bool = True
    ) -> bool:
        """
        Validate wallet stake amount.

        Args:
            wallet_address: Wallet to validate
            min_stake: Minimum required stake
            use_cache: Whether to use cached results

        Returns:
            True if stake is sufficient
        """
        # Mock stake validation for testing
        # In production, this would call actual Sui RPC

        # Simulate some wallets having different stake amounts
        test_stakes = {
            "0x1111111111111111111111111111111111111111": 2000000000,  # 2 SUI
            "0x2222222222222222222222222222222222222222": 1000000000,  # 1 SUI
            "0x3333333333333333333333333333333333333333": 500000000,   # 0.5 SUI
        }

        wallet_stake = test_stakes.get(wallet_address, 1500000000)  # Default 1.5 SUI

        # Simulate network delay
        await asyncio.sleep(0.1 if use_cache else 0.5)

        return wallet_stake >= min_stake

    async def register_agent(self, config: Dict[str, Any]) -> Dict[str, Any]:
        """
        Register a DaaS agent with the cluster.

        Args:
            config: Agent configuration

        Returns:
            Registration result
        """
        with TestTimer("Agent Registration"):
            # Validate configuration
            required_fields = ["server_url", "token", "node_name"]
            for field in required_fields:
                if field not in config:
                    raise ValueError(f"Missing required field: {field}")

            # Check if token is Seal format
            is_seal_token = config["token"].startswith("SEAL")

            if is_seal_token and config.get("daas_enabled", True):
                # DaaS registration path
                return await self._register_with_daas(config)
            else:
                # Traditional registration path
                return await self._register_traditional(config)

    async def _register_with_daas(self, config: Dict[str, Any]) -> Dict[str, Any]:
        """Register agent using DaaS authentication."""
        # Parse Seal token
        token = config["token"]
        if not token.startswith("SEAL"):
            raise ValueError("Invalid Seal token format")

        # Extract wallet address from token
        parts = token[4:].split("::")  # Remove "SEAL" prefix
        if len(parts) != 3:
            raise ValueError("Invalid Seal token structure")

        wallet_address = parts[0]
        signature = parts[1]
        challenge = parts[2]

        # Validate stake
        min_stake = config.get("min_stake", 1000000000)
        stake_valid = await self.validate_stake(wallet_address, min_stake)

        if not stake_valid:
            raise Exception("Insufficient stake for DaaS registration")

        # Validate signature (simplified for testing)
        expected_message = f"{challenge}:{int(time.time())}:{wallet_address}"
        # In production, verify actual cryptographic signature

        # Simulate successful registration
        await asyncio.sleep(2)  # Simulate registration time

        return {
            "status": "success",
            "node_name": config["node_name"],
            "authentication_method": "daas-seal",
            "wallet_address": wallet_address,
            "stake_validated": True,
            "registration_time": time.time()
        }

    async def _register_traditional(self, config: Dict[str, Any]) -> Dict[str, Any]:
        """Register agent using traditional authentication."""
        # Simulate traditional K3s registration
        await asyncio.sleep(1)

        return {
            "status": "success",
            "node_name": config["node_name"],
            "authentication_method": "traditional",
            "registration_time": time.time()
        }

    async def start_stake_monitoring(
        self,
        wallet_address: str,
        check_interval: int = 30
    ) -> Dict[str, Any]:
        """
        Start monitoring stake changes for a wallet.

        Args:
            wallet_address: Wallet to monitor
            check_interval: Check interval in seconds

        Returns:
            Monitoring status
        """
        if wallet_address in self._stake_monitors:
            raise ValueError(f"Already monitoring wallet {wallet_address}")

        # Create monitoring task
        monitor_task = asyncio.create_task(
            self._stake_monitor_loop(wallet_address, check_interval)
        )

        self._stake_monitors[wallet_address] = {
            "task": monitor_task,
            "status": "valid",
            "last_check": time.time(),
            "check_interval": check_interval
        }

        return {"status": "started", "wallet_address": wallet_address}

    async def _stake_monitor_loop(self, wallet_address: str, check_interval: int):
        """Monitoring loop for stake changes."""
        while wallet_address in self._stake_monitors:
            try:
                # Check current stake
                is_valid = await self.validate_stake(wallet_address, 1000000000)

                # Update status
                self._stake_monitors[wallet_address]["status"] = "valid" if is_valid else "invalid"
                self._stake_monitors[wallet_address]["last_check"] = time.time()

                await asyncio.sleep(check_interval)

            except asyncio.CancelledError:
                break
            except Exception as e:
                print(f"Stake monitoring error for {wallet_address}: {e}")
                await asyncio.sleep(check_interval)

    async def stop_stake_monitoring(self, wallet_address: str):
        """Stop monitoring a wallet."""
        if wallet_address in self._stake_monitors:
            monitor_info = self._stake_monitors[wallet_address]
            monitor_info["task"].cancel()
            del self._stake_monitors[wallet_address]

    def get_last_stake_status(self, wallet_address: str) -> str:
        """Get the last known stake status for a wallet."""
        return self._stake_monitors.get(wallet_address, {}).get("status", "unknown")

    async def validate_stake_with_performance(
        self,
        wallet_address: str,
        base_min_stake: int,
        performance_adjustment: bool = True
    ) -> bool:
        """
        Validate stake with performance score adjustment.

        Args:
            wallet_address: Wallet to validate
            base_min_stake: Base minimum stake requirement
            performance_adjustment: Whether to adjust for performance

        Returns:
            True if stake meets adjusted requirements
        """
        # Get performance score (mock data)
        performance_scores = {
            "0x8888888888888888888888888888888888888888": 95,  # Good
            "0x9999999999999999999999999999999999999999": 85,  # Average
        }

        performance_score = performance_scores.get(wallet_address, 90)

        if performance_adjustment:
            # Adjust requirements based on performance
            if performance_score >= 95:
                adjusted_min_stake = int(base_min_stake * 0.9)  # 10% discount
            elif performance_score >= 85:
                adjusted_min_stake = base_min_stake
            else:
                adjusted_min_stake = int(base_min_stake * 1.2)  # 20% penalty
        else:
            adjusted_min_stake = base_min_stake

        return await self.validate_stake(wallet_address, adjusted_min_stake)

    async def deploy_from_walrus(
        self,
        blob_id: str,
        namespace: str = "default",
        deployment_type: str = "application"
    ) -> Dict[str, Any]:
        """
        Deploy an application from Walrus blob.

        Args:
            blob_id: Walrus blob identifier
            namespace: Kubernetes namespace
            deployment_type: Type of deployment

        Returns:
            Deployment result
        """
        # Simulate Walrus deployment
        await asyncio.sleep(3)  # Simulate deployment time

        return {
            "status": "success",
            "blob_id": blob_id,
            "namespace": namespace,
            "resource_type": "ConfigMap" if deployment_type == "configmap" else "Deployment",
            "deployment_time": time.time()
        }

    async def deploy_application_from_walrus(
        self,
        blob_id: str,
        config: Dict[str, Any],
        timeout: int = 300
    ) -> Dict[str, Any]:
        """
        Deploy a complete application from Walrus.

        Args:
            blob_id: Walrus blob identifier
            config: Deployment configuration
            timeout: Deployment timeout

        Returns:
            Deployment result
        """
        app_name = config.get("name", "unknown-app")

        # Simulate deployment process
        await asyncio.sleep(5)  # Simulate deployment time

        return {
            "status": "success",
            "deployment_name": app_name,
            "blob_id": blob_id,
            "namespace": config.get("namespace", "default"),
            "deployment_time": time.time()
        }

    async def update_application_from_walrus(
        self,
        app_name: str,
        blob_id: str,
        namespace: str = "default",
        version: str = "latest"
    ) -> Dict[str, Any]:
        """
        Update an existing application with new Walrus blob.

        Args:
            app_name: Application name
            blob_id: New blob identifier
            namespace: Kubernetes namespace
            version: Application version

        Returns:
            Update result
        """
        # Simulate update process
        await asyncio.sleep(3)

        return {
            "status": "success",
            "app_name": app_name,
            "previous_version": "1.0.0",  # Mock previous version
            "new_version": version,
            "blob_id": blob_id,
            "update_time": time.time()
        }

    async def rollback_application(
        self,
        app_name: str,
        target_blob_id: str,
        namespace: str = "default"
    ) -> Dict[str, Any]:
        """
        Rollback an application to a previous version.

        Args:
            app_name: Application name
            target_blob_id: Target blob identifier
            namespace: Kubernetes namespace

        Returns:
            Rollback result
        """
        # Simulate rollback process
        await asyncio.sleep(2)

        return {
            "status": "success",
            "app_name": app_name,
            "target_blob_id": target_blob_id,
            "target_version": "1.0.0",  # Mock target version
            "rollback_time": time.time()
        }

    async def restart_agent(self, node_name: str):
        """
        Restart a DaaS agent (simulation).

        Args:
            node_name: Name of the node to restart
        """
        # Simulate agent restart
        await asyncio.sleep(2)

    async def cleanup(self):
        """Clean up DaaS client resources."""
        # Stop all stake monitors
        for wallet_address in list(self._stake_monitors.keys()):
            await self.stop_stake_monitoring(wallet_address)

        # Close HTTP session
        if self.session:
            await self.session.close()
            self.session = None