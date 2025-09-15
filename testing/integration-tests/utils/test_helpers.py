"""
Common test utilities and helper functions for DaaS integration tests.
"""

import asyncio
import time
import logging
from typing import Callable, Any, Optional
from functools import wraps

logger = logging.getLogger(__name__)


async def wait_for_condition(
    condition: Callable[[], bool],
    timeout: int = 60,
    interval: int = 2,
    description: str = "condition"
) -> bool:
    """
    Wait for a condition to become true within a timeout period.

    Args:
        condition: Function that returns True when condition is met
        timeout: Maximum time to wait in seconds
        interval: How often to check the condition in seconds
        description: Description of what we're waiting for

    Returns:
        True if condition was met, False if timeout occurred

    Raises:
        TimeoutError: If condition is not met within timeout
    """
    start_time = time.time()

    while time.time() - start_time < timeout:
        try:
            if condition():
                logger.info(f"✓ {description} - condition met")
                return True
        except Exception as e:
            logger.debug(f"Condition check failed: {e}")

        await asyncio.sleep(interval)

    logger.error(f"✗ {description} - timeout after {timeout}s")
    raise TimeoutError(f"Timeout waiting for {description} after {timeout} seconds")


def retry_async(max_attempts: int = 3, delay: float = 1.0, backoff: float = 2.0):
    """
    Decorator to retry async functions on failure.

    Args:
        max_attempts: Maximum number of attempts
        delay: Initial delay between attempts
        backoff: Multiplier for delay on each retry
    """
    def decorator(func):
        @wraps(func)
        async def wrapper(*args, **kwargs):
            current_delay = delay
            last_exception = None

            for attempt in range(max_attempts):
                try:
                    return await func(*args, **kwargs)
                except Exception as e:
                    last_exception = e
                    if attempt == max_attempts - 1:
                        logger.error(f"Function {func.__name__} failed after {max_attempts} attempts")
                        raise e

                    logger.warning(f"Attempt {attempt + 1} failed for {func.__name__}: {e}")
                    await asyncio.sleep(current_delay)
                    current_delay *= backoff

            raise last_exception

        return wrapper
    return decorator


class TestTimer:
    """Context manager for timing test operations."""

    def __init__(self, name: str):
        self.name = name
        self.start_time = None
        self.end_time = None

    def __enter__(self):
        self.start_time = time.time()
        logger.info(f"⏱️  Starting {self.name}")
        return self

    def __exit__(self, exc_type, exc_val, exc_tb):
        self.end_time = time.time()
        duration = self.end_time - self.start_time

        if exc_type is None:
            logger.info(f"✓ {self.name} completed in {duration:.2f}s")
        else:
            logger.error(f"✗ {self.name} failed after {duration:.2f}s")

    @property
    def duration(self) -> Optional[float]:
        if self.start_time and self.end_time:
            return self.end_time - self.start_time
        return None


async def wait_for_service_ready(
    health_check: Callable[[], bool],
    service_name: str,
    timeout: int = 120,
    interval: int = 5
) -> bool:
    """
    Wait for a service to become ready.

    Args:
        health_check: Function that returns True if service is healthy
        service_name: Name of the service for logging
        timeout: Maximum time to wait
        interval: How often to check

    Returns:
        True if service becomes ready

    Raises:
        TimeoutError: If service doesn't become ready within timeout
    """
    return await wait_for_condition(
        condition=health_check,
        timeout=timeout,
        interval=interval,
        description=f"{service_name} to be ready"
    )


class ResourceTracker:
    """Track and cleanup test resources."""

    def __init__(self):
        self.resources = []

    def add_resource(self, resource_type: str, identifier: str, cleanup_func: Callable):
        """Add a resource to be tracked and cleaned up."""
        self.resources.append({
            'type': resource_type,
            'id': identifier,
            'cleanup': cleanup_func
        })
        logger.debug(f"Tracking {resource_type}: {identifier}")

    async def cleanup_all(self):
        """Clean up all tracked resources."""
        logger.info(f"Cleaning up {len(self.resources)} resources")

        for resource in reversed(self.resources):  # Cleanup in reverse order
            try:
                await resource['cleanup']()
                logger.debug(f"Cleaned up {resource['type']}: {resource['id']}")
            except Exception as e:
                logger.warning(f"Failed to cleanup {resource['type']} {resource['id']}: {e}")

        self.resources.clear()


async def poll_until_success(
    async_func: Callable,
    success_condition: Callable[[Any], bool],
    max_attempts: int = 10,
    delay: float = 2.0,
    description: str = "operation"
) -> Any:
    """
    Poll an async function until it succeeds based on a condition.

    Args:
        async_func: The async function to call
        success_condition: Function to check if result is successful
        max_attempts: Maximum number of attempts
        delay: Delay between attempts
        description: Description for logging

    Returns:
        The successful result

    Raises:
        Exception: If all attempts fail
    """
    for attempt in range(max_attempts):
        try:
            result = await async_func()
            if success_condition(result):
                logger.info(f"✓ {description} succeeded on attempt {attempt + 1}")
                return result
        except Exception as e:
            logger.debug(f"Attempt {attempt + 1} failed: {e}")

        if attempt < max_attempts - 1:
            await asyncio.sleep(delay)

    raise Exception(f"Failed to achieve success condition for {description} after {max_attempts} attempts")


def generate_test_id(prefix: str = "test") -> str:
    """Generate a unique test identifier."""
    import uuid
    return f"{prefix}-{uuid.uuid4().hex[:8]}"


async def verify_resource_exists(
    check_func: Callable[[], Any],
    resource_name: str,
    timeout: int = 30
) -> bool:
    """
    Verify that a resource exists by polling.

    Args:
        check_func: Function that checks if resource exists
        resource_name: Name of resource for logging
        timeout: How long to wait

    Returns:
        True if resource exists
    """
    try:
        await wait_for_condition(
            condition=lambda: check_func() is not None,
            timeout=timeout,
            interval=2,
            description=f"{resource_name} to exist"
        )
        return True
    except TimeoutError:
        return False


class LogCapture:
    """Capture and analyze logs during tests."""

    def __init__(self):
        self.logs = []
        self.handler = None

    def start_capture(self, logger_name: str = None):
        """Start capturing logs."""
        import logging

        self.handler = logging.StreamHandler()
        self.handler.emit = self._capture_log

        if logger_name:
            target_logger = logging.getLogger(logger_name)
        else:
            target_logger = logging.getLogger()

        target_logger.addHandler(self.handler)

    def _capture_log(self, record):
        """Capture a log record."""
        self.logs.append({
            'timestamp': time.time(),
            'level': record.levelname,
            'message': record.getMessage(),
            'logger': record.name
        })

    def stop_capture(self):
        """Stop capturing logs."""
        if self.handler:
            logging.getLogger().removeHandler(self.handler)
            self.handler = None

    def get_logs(self, level: str = None, contains: str = None) -> list:
        """Get captured logs with optional filtering."""
        filtered_logs = self.logs

        if level:
            filtered_logs = [log for log in filtered_logs if log['level'] == level]

        if contains:
            filtered_logs = [log for log in filtered_logs if contains in log['message']]

        return filtered_logs

    def assert_log_contains(self, message: str, level: str = None):
        """Assert that a log message was captured."""
        matching_logs = self.get_logs(level=level, contains=message)
        assert len(matching_logs) > 0, f"No log found containing '{message}'"


async def run_parallel_tests(test_functions: list, max_concurrent: int = 5) -> list:
    """
    Run multiple test functions in parallel with concurrency control.

    Args:
        test_functions: List of async test functions to run
        max_concurrent: Maximum concurrent tests

    Returns:
        List of results from all tests
    """
    semaphore = asyncio.Semaphore(max_concurrent)

    async def run_with_semaphore(test_func):
        async with semaphore:
            return await test_func()

    tasks = [run_with_semaphore(test_func) for test_func in test_functions]
    return await asyncio.gather(*tasks, return_exceptions=True)


def assert_no_errors_in_logs(logs: list):
    """Assert that no error-level logs were captured."""
    error_logs = [log for log in logs if log['level'] == 'ERROR']
    if error_logs:
        error_messages = [log['message'] for log in error_logs]
        raise AssertionError(f"Found {len(error_logs)} error logs: {error_messages}")


async def verify_cleanup_complete(resource_list: list, check_func: Callable[[str], bool]):
    """
    Verify that all resources in a list have been cleaned up.

    Args:
        resource_list: List of resource identifiers
        check_func: Function that returns False if resource is cleaned up
    """
    for resource_id in resource_list:
        try:
            await wait_for_condition(
                condition=lambda rid=resource_id: not check_func(rid),
                timeout=30,
                interval=2,
                description=f"cleanup of {resource_id}"
            )
        except TimeoutError:
            logger.warning(f"Resource {resource_id} may not have been properly cleaned up")


class PerformanceTracker:
    """Track performance metrics during tests."""

    def __init__(self):
        self.metrics = {}

    def start_timer(self, metric_name: str):
        """Start timing a metric."""
        self.metrics[metric_name] = {'start': time.time()}

    def end_timer(self, metric_name: str):
        """End timing a metric."""
        if metric_name in self.metrics:
            self.metrics[metric_name]['end'] = time.time()
            self.metrics[metric_name]['duration'] = (
                self.metrics[metric_name]['end'] - self.metrics[metric_name]['start']
            )

    def get_duration(self, metric_name: str) -> float:
        """Get the duration for a metric."""
        return self.metrics.get(metric_name, {}).get('duration', 0.0)

    def assert_performance(self, metric_name: str, max_duration: float):
        """Assert that a metric meets performance requirements."""
        duration = self.get_duration(metric_name)
        assert duration <= max_duration, (
            f"Performance requirement failed: {metric_name} took {duration:.2f}s, "
            f"expected <= {max_duration}s"
        )

    def get_summary(self) -> dict:
        """Get a summary of all performance metrics."""
        return {
            name: data.get('duration', 0.0)
            for name, data in self.metrics.items()
        }