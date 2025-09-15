"""
Stake Verification Integration Tests

Tests the Sui blockchain stake verification system including:
- Stake amount validation
- Worker eligibility checks
- Dynamic stake monitoring
- Slashing detection
"""

import pytest
import asyncio
import time
from decimal import Decimal

from .utils.sui_client import SuiTestClient
from .utils.daas_client import DaaSClient
from .utils.test_helpers import wait_for_condition, retry_async


class TestStakeVerification:
    """Test suite for Sui blockchain stake verification."""

    @pytest.fixture(autouse=True)
    async def setup_test_environment(self):
        """Set up test environment before each test."""
        self.sui_client = SuiTestClient()
        self.daas_client = DaaSClient()

        # Ensure Sui node is ready
        await wait_for_condition(
            lambda: self.sui_client.is_healthy(),
            timeout=60,
            interval=5,
            description="Waiting for Sui node to be ready"
        )

        yield

        # Cleanup test wallets
        await self._cleanup_test_wallets()

    async def _cleanup_test_wallets(self):
        """Clean up test wallets after each test."""
        test_wallets = [
            "0x1111111111111111111111111111111111111111",
            "0x2222222222222222222222222222222222222222",
            "0x3333333333333333333333333333333333333333",
            "0x4444444444444444444444444444444444444444",
            "0x5555555555555555555555555555555555555555"
        ]

        for wallet in test_wallets:
            try:
                await self.sui_client.cleanup_test_wallet(wallet)
            except Exception as e:
                print(f"Warning: Failed to cleanup wallet {wallet}: {e}")

    @pytest.mark.asyncio
    async def test_minimum_stake_validation(self):
        """Test validation of minimum stake requirements."""

        test_cases = [
            {
                "wallet": "0x1111111111111111111111111111111111111111",
                "stake": 500000000,  # 0.5 SUI - below minimum
                "expected_valid": False,
                "description": "Below minimum stake"
            },
            {
                "wallet": "0x2222222222222222222222222222222222222222",
                "stake": 1000000000,  # 1 SUI - exactly minimum
                "expected_valid": True,
                "description": "Exactly minimum stake"
            },
            {
                "wallet": "0x3333333333333333333333333333333333333333",
                "stake": 2000000000,  # 2 SUI - above minimum
                "expected_valid": True,
                "description": "Above minimum stake"
            }
        ]

        for test_case in test_cases:
            wallet = test_case["wallet"]
            stake_amount = test_case["stake"]
            expected_valid = test_case["expected_valid"]
            description = test_case["description"]

            # Setup stake on Sui
            await self.sui_client.setup_test_stake(wallet, stake_amount)

            # Validate stake through DaaS client
            is_valid = await self.daas_client.validate_stake(
                wallet_address=wallet,
                min_stake=1000000000  # 1 SUI minimum
            )

            assert is_valid == expected_valid, f"Failed for {description}"

            # Get detailed stake info
            stake_info = await self.sui_client.get_stake_info(wallet)
            assert stake_info["wallet_address"] == wallet
            assert stake_info["stake_amount"] == stake_amount

            if expected_valid:
                assert stake_info["status"] == 1  # Active
            else:
                # Should either be inactive or validation should fail
                assert stake_info["status"] != 1 or not is_valid

    @pytest.mark.asyncio
    async def test_worker_status_validation(self):
        """Test validation of different worker status states."""

        wallet_address = "0x4444444444444444444444444444444444444444"
        stake_amount = 2000000000  # Sufficient stake

        status_test_cases = [
            {"status": 0, "name": "inactive", "expected_valid": False},
            {"status": 1, "name": "active", "expected_valid": True},
            {"status": 2, "name": "suspended", "expected_valid": False},
            {"status": 3, "name": "slashed", "expected_valid": False}
        ]

        for test_case in status_test_cases:
            status = test_case["status"]
            status_name = test_case["name"]
            expected_valid = test_case["expected_valid"]

            # Setup worker with specific status
            await self.sui_client.setup_test_stake(
                wallet_address,
                stake_amount,
                status=status
            )

            # Validate worker eligibility
            is_valid = await self.daas_client.validate_stake(
                wallet_address=wallet_address,
                min_stake=1000000000
            )

            assert is_valid == expected_valid, f"Failed for status {status_name}"

            # Verify status in stake info
            stake_info = await self.sui_client.get_stake_info(wallet_address)
            assert stake_info["status"] == status

    @pytest.mark.asyncio
    async def test_stake_amount_precision(self):
        """Test stake validation with various precision levels."""

        wallet_address = "0x5555555555555555555555555555555555555555"

        precision_test_cases = [
            {"stake": 999999999, "expected_valid": False},  # 1 MIST below minimum
            {"stake": 1000000000, "expected_valid": True},  # Exactly 1 SUI
            {"stake": 1000000001, "expected_valid": True},  # 1 MIST above minimum
            {"stake": 1500000000, "expected_valid": True},  # 1.5 SUI
            {"stake": 10000000000, "expected_valid": True}, # 10 SUI
        ]

        for test_case in precision_test_cases:
            stake_amount = test_case["stake"]
            expected_valid = test_case["expected_valid"]

            await self.sui_client.setup_test_stake(wallet_address, stake_amount)

            is_valid = await self.daas_client.validate_stake(
                wallet_address=wallet_address,
                min_stake=1000000000
            )

            assert is_valid == expected_valid, \
                f"Failed for stake amount {stake_amount} MIST"

            # Verify exact amount
            stake_info = await self.sui_client.get_stake_info(wallet_address)
            assert stake_info["stake_amount"] == stake_amount

    @pytest.mark.asyncio
    async def test_dynamic_stake_monitoring(self):
        """Test dynamic monitoring of stake changes."""

        wallet_address = "0x6666666666666666666666666666666666666666"
        initial_stake = 2000000000  # 2 SUI

        # Setup initial stake
        await self.sui_client.setup_test_stake(wallet_address, initial_stake)

        # Verify initial state
        is_valid = await self.daas_client.validate_stake(
            wallet_address=wallet_address,
            min_stake=1000000000
        )
        assert is_valid

        # Start monitoring stake changes
        stake_monitor = await self.daas_client.start_stake_monitoring(
            wallet_address=wallet_address,
            check_interval=1  # Check every second for testing
        )

        try:
            # Simulate stake reduction below minimum
            reduced_stake = 500000000  # 0.5 SUI
            await self.sui_client.update_stake(wallet_address, reduced_stake)

            # Wait for monitoring to detect the change
            await wait_for_condition(
                lambda: self.daas_client.get_last_stake_status(wallet_address) == "invalid",
                timeout=10,
                interval=0.5,
                description="Waiting for stake reduction detection"
            )

            # Verify stake is now invalid
            is_valid = await self.daas_client.validate_stake(
                wallet_address=wallet_address,
                min_stake=1000000000
            )
            assert not is_valid

            # Simulate stake increase back above minimum
            increased_stake = 3000000000  # 3 SUI
            await self.sui_client.update_stake(wallet_address, increased_stake)

            # Wait for monitoring to detect the recovery
            await wait_for_condition(
                lambda: self.daas_client.get_last_stake_status(wallet_address) == "valid",
                timeout=10,
                interval=0.5,
                description="Waiting for stake increase detection"
            )

            # Verify stake is valid again
            is_valid = await self.daas_client.validate_stake(
                wallet_address=wallet_address,
                min_stake=1000000000
            )
            assert is_valid

        finally:
            # Stop monitoring
            await self.daas_client.stop_stake_monitoring(wallet_address)

    @pytest.mark.asyncio
    async def test_slashing_detection(self):
        """Test detection of slashed workers."""

        wallet_address = "0x7777777777777777777777777777777777777777"
        stake_amount = 5000000000  # 5 SUI

        # Setup active worker with good stake
        await self.sui_client.setup_test_stake(
            wallet_address,
            stake_amount,
            status=1  # Active
        )

        # Verify initial valid state
        is_valid = await self.daas_client.validate_stake(
            wallet_address=wallet_address,
            min_stake=1000000000
        )
        assert is_valid

        worker_info = await self.sui_client.get_worker_info(wallet_address)
        assert worker_info["status"] == 1  # Active

        # Simulate slashing event
        await self.sui_client.slash_worker(
            wallet_address=wallet_address,
            slashed_amount=2000000000,  # Slash 2 SUI
            reason="misbehavior_detected"
        )

        # Verify worker is now slashed
        await wait_for_condition(
            lambda: self._check_worker_slashed(wallet_address),
            timeout=30,
            interval=2,
            description="Waiting for slashing to be processed"
        )

        # Verify stake validation fails due to slashed status
        is_valid = await self.daas_client.validate_stake(
            wallet_address=wallet_address,
            min_stake=1000000000
        )
        assert not is_valid

        # Verify updated worker info
        worker_info = await self.sui_client.get_worker_info(wallet_address)
        assert worker_info["status"] == 3  # Slashed
        assert worker_info["stake_amount"] == 3000000000  # Original 5 SUI - 2 SUI slashed

    async def _check_worker_slashed(self, wallet_address: str) -> bool:
        """Check if a worker has been slashed."""
        try:
            worker_info = await self.sui_client.get_worker_info(wallet_address)
            return worker_info["status"] == 3  # Slashed status
        except Exception:
            return False

    @pytest.mark.asyncio
    async def test_performance_score_impact(self):
        """Test how performance scores affect stake validation."""

        wallet_address = "0x8888888888888888888888888888888888888888"
        stake_amount = 2000000000  # 2 SUI

        performance_test_cases = [
            {"score": 95, "expected_multiplier": 1.0},  # Good performance
            {"score": 85, "expected_multiplier": 0.9},  # Average performance
            {"score": 70, "expected_multiplier": 0.8},  # Below average
            {"score": 50, "expected_multiplier": 0.6},  # Poor performance
        ]

        for test_case in performance_test_cases:
            performance_score = test_case["score"]
            expected_multiplier = test_case["expected_multiplier"]

            # Setup worker with specific performance score
            await self.sui_client.setup_test_worker(
                wallet_address=wallet_address,
                stake_amount=stake_amount,
                performance_score=performance_score
            )

            # Validate with performance-adjusted requirements
            effective_min_stake = int(1000000000 / expected_multiplier)

            is_valid = await self.daas_client.validate_stake_with_performance(
                wallet_address=wallet_address,
                base_min_stake=1000000000,
                performance_adjustment=True
            )

            # Workers with good performance should pass with lower effective requirements
            # Workers with poor performance need higher stakes
            expected_valid = stake_amount >= effective_min_stake

            assert is_valid == expected_valid, \
                f"Failed for performance score {performance_score}"

            # Verify performance score in worker info
            worker_info = await self.sui_client.get_worker_info(wallet_address)
            assert worker_info["performance_score"] == performance_score

    @pytest.mark.asyncio
    async def test_concurrent_stake_validations(self):
        """Test concurrent stake validations for multiple workers."""

        num_workers = 10
        base_wallet = "0xa000000000000000000000000000000000000"

        # Setup multiple workers concurrently
        setup_tasks = []
        for i in range(num_workers):
            wallet_address = f"{base_wallet}{i:02d}"
            stake_amount = 1000000000 + (i * 100000000)  # Varying stakes

            task = self.sui_client.setup_test_stake(wallet_address, stake_amount)
            setup_tasks.append(task)

        await asyncio.gather(*setup_tasks)

        # Validate all stakes concurrently
        validation_tasks = []
        for i in range(num_workers):
            wallet_address = f"{base_wallet}{i:02d}"
            task = self.daas_client.validate_stake(
                wallet_address=wallet_address,
                min_stake=1000000000
            )
            validation_tasks.append(task)

        results = await asyncio.gather(*validation_tasks)

        # All should be valid (all have at least minimum stake)
        for i, is_valid in enumerate(results):
            assert is_valid, f"Worker {i} stake validation failed"

        # Verify we can get detailed info for all workers
        info_tasks = []
        for i in range(num_workers):
            wallet_address = f"{base_wallet}{i:02d}"
            task = self.sui_client.get_stake_info(wallet_address)
            info_tasks.append(task)

        stake_infos = await asyncio.gather(*info_tasks)

        for i, stake_info in enumerate(stake_infos):
            expected_stake = 1000000000 + (i * 100000000)
            assert stake_info["stake_amount"] == expected_stake

    @pytest.mark.asyncio
    async def test_stake_validation_caching(self):
        """Test stake validation caching behavior."""

        wallet_address = "0x9999999999999999999999999999999999999999"
        stake_amount = 2000000000

        await self.sui_client.setup_test_stake(wallet_address, stake_amount)

        # First validation (should hit blockchain)
        start_time = time.time()
        is_valid1 = await self.daas_client.validate_stake(
            wallet_address=wallet_address,
            min_stake=1000000000,
            use_cache=True
        )
        first_duration = time.time() - start_time

        assert is_valid1

        # Second validation (should use cache)
        start_time = time.time()
        is_valid2 = await self.daas_client.validate_stake(
            wallet_address=wallet_address,
            min_stake=1000000000,
            use_cache=True
        )
        second_duration = time.time() - start_time

        assert is_valid2
        # Cached validation should be significantly faster
        assert second_duration < first_duration * 0.5

        # Third validation with cache disabled (should hit blockchain again)
        start_time = time.time()
        is_valid3 = await self.daas_client.validate_stake(
            wallet_address=wallet_address,
            min_stake=1000000000,
            use_cache=False
        )
        third_duration = time.time() - start_time

        assert is_valid3
        # Non-cached validation should take longer than cached
        assert third_duration > second_duration