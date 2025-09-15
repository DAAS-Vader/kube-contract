"""
Nautilus Attestation Integration Tests

Tests the Nautilus attestation system including:
- Code attestation verification
- Runtime integrity monitoring
- Deployment validation
- Compliance checking
"""

import pytest
import asyncio
import hashlib
import json
import time
from datetime import datetime, timedelta

from .utils.nautilus_client import NautilusTestClient
from .utils.walrus_client import WalrusTestClient
from .utils.k3s_client import K3sClient
from .utils.daas_client import DaaSClient
from .utils.test_helpers import wait_for_condition, retry_async


class TestNautilusAttestation:
    """Test suite for Nautilus attestation system."""

    @pytest.fixture(autouse=True)
    async def setup_test_environment(self):
        """Set up test environment before each test."""
        self.nautilus_client = NautilusTestClient()
        self.walrus_client = WalrusTestClient()
        self.k3s_client = K3sClient()
        self.daas_client = DaaSClient()

        # Ensure Nautilus attestation service is ready
        await wait_for_condition(
            lambda: self.nautilus_client.is_healthy(),
            timeout=60,
            interval=5,
            description="Waiting for Nautilus attestation service to be ready"
        )

        yield

        # Cleanup test attestations and deployments
        await self._cleanup_test_resources()

    async def _cleanup_test_resources(self):
        """Clean up test resources after each test."""
        try:
            # Clean up test deployments
            deployments = await self.k3s_client.list_deployments(
                namespace="default",
                label_selector="nautilus.io/test=true"
            )
            for deployment in deployments:
                await self.k3s_client.delete_deployment(
                    deployment.metadata.name,
                    "default"
                )

            # Clean up attestation records
            await self.nautilus_client.cleanup_test_attestations()

        except Exception as e:
            print(f"Warning: Failed to cleanup test resources: {e}")

    @pytest.mark.asyncio
    async def test_code_attestation_creation(self):
        """Test creation of code attestation records."""

        # Prepare test code
        test_code = """
        const express = require('express');
        const app = express();

        app.get('/health', (req, res) => {
            res.json({
                status: 'healthy',
                version: '1.0.0',
                attestation: 'verified'
            });
        });

        app.listen(3000, () => {
            console.log('Attested app running on port 3000');
        });
        """

        # Calculate code hash
        code_hash = hashlib.sha256(test_code.encode('utf-8')).hexdigest()

        # Store code in Walrus
        blob_id = await self.walrus_client.store_blob(
            data=test_code.encode('utf-8'),
            metadata={
                "name": "attested-app",
                "version": "1.0.0",
                "type": "nodejs"
            }
        )

        # Create attestation record
        attestation_request = {
            "code_hash": code_hash,
            "blob_id": blob_id,
            "metadata": {
                "name": "attested-app",
                "version": "1.0.0",
                "developer": "test-developer",
                "build_timestamp": datetime.utcnow().isoformat(),
                "dependencies": ["express@4.18.0"],
                "security_scan": "passed",
                "compliance_level": "standard"
            },
            "attestor": {
                "name": "test-attestor",
                "public_key": "0x1234567890abcdef1234567890abcdef12345678",
                "reputation_score": 95
            }
        }

        attestation_result = await self.nautilus_client.create_attestation(
            attestation_request
        )

        assert attestation_result["status"] == "success"
        assert "attestation_id" in attestation_result
        assert attestation_result["code_hash"] == code_hash
        assert attestation_result["blob_id"] == blob_id

        attestation_id = attestation_result["attestation_id"]

        # Verify attestation was stored
        stored_attestation = await self.nautilus_client.get_attestation(attestation_id)

        assert stored_attestation is not None
        assert stored_attestation["code_hash"] == code_hash
        assert stored_attestation["blob_id"] == blob_id
        assert stored_attestation["metadata"]["name"] == "attested-app"
        assert stored_attestation["metadata"]["security_scan"] == "passed"

    @pytest.mark.asyncio
    async def test_deployment_with_attestation_verification(self):
        """Test deployment with mandatory attestation verification."""

        # Create and attest code first
        app_code = """
        const express = require('express');
        const app = express();

        app.get('/attestation', (req, res) => {
            res.json({
                message: 'This code has been attested',
                attestation_verified: true,
                timestamp: new Date().toISOString()
            });
        });

        app.listen(3000);
        """

        code_hash = hashlib.sha256(app_code.encode('utf-8')).hexdigest()

        # Store in Walrus
        blob_id = await self.walrus_client.store_blob(
            data=app_code.encode('utf-8'),
            metadata={"name": "verified-app", "version": "1.0.0"}
        )

        # Create attestation
        attestation_request = {
            "code_hash": code_hash,
            "blob_id": blob_id,
            "metadata": {
                "name": "verified-app",
                "version": "1.0.0",
                "security_scan": "passed",
                "vulnerability_count": 0,
                "compliance_level": "high",
                "attestation_level": "production"
            },
            "attestor": {
                "name": "production-attestor",
                "public_key": "0xabcdef1234567890abcdef1234567890abcdef12",
                "reputation_score": 98,
                "certification": "iso27001"
            }
        }

        attestation_result = await self.nautilus_client.create_attestation(
            attestation_request
        )
        attestation_id = attestation_result["attestation_id"]

        # Deploy with attestation requirement
        deployment_manifest = {
            "apiVersion": "apps/v1",
            "kind": "Deployment",
            "metadata": {
                "name": "verified-app",
                "labels": {
                    "app": "verified-app",
                    "nautilus.io/test": "true"
                },
                "annotations": {
                    "nautilus.io/attestation-required": "true",
                    "nautilus.io/attestation-id": attestation_id,
                    "nautilus.io/compliance-level": "high",
                    "daas.io/walrus-blob-id": blob_id
                }
            },
            "spec": {
                "replicas": 1,
                "selector": {"matchLabels": {"app": "verified-app"}},
                "template": {
                    "metadata": {
                        "labels": {"app": "verified-app"},
                        "annotations": {
                            "nautilus.io/attestation-id": attestation_id,
                            "nautilus.io/verification-required": "true"
                        }
                    },
                    "spec": {
                        "initContainers": [{
                            "name": "attestation-verifier",
                            "image": "nautilus/verifier:latest",
                            "env": [
                                {"name": "NAUTILUS_ENDPOINT", "value": "http://nautilus-attestation:8090"},
                                {"name": "ATTESTATION_ID", "value": attestation_id},
                                {"name": "BLOB_ID", "value": blob_id},
                                {"name": "REQUIRED_COMPLIANCE", "value": "high"}
                            ],
                            "volumeMounts": [{
                                "name": "verification-status",
                                "mountPath": "/verification"
                            }]
                        }],
                        "containers": [{
                            "name": "app",
                            "image": "node:18-alpine",
                            "command": ["sh", "-c"],
                            "args": [
                                "if [ ! -f /verification/verified ]; then exit 1; fi && node /app/code.js"
                            ],
                            "volumeMounts": [
                                {
                                    "name": "verification-status",
                                    "mountPath": "/verification"
                                },
                                {
                                    "name": "app-code",
                                    "mountPath": "/app"
                                }
                            ]
                        }],
                        "volumes": [
                            {"name": "verification-status", "emptyDir": {}},
                            {"name": "app-code", "emptyDir": {}}
                        ]
                    }
                }
            }
        }

        # Deploy the attested application
        deployment = await self.k3s_client.create_deployment(
            deployment_manifest,
            namespace="default"
        )

        assert deployment is not None

        # Wait for deployment to be ready (includes attestation verification)
        await wait_for_condition(
            lambda: self._check_deployment_ready("verified-app"),
            timeout=180,
            interval=10,
            description="Waiting for attested deployment to be ready"
        )

        # Verify deployment is running with attestation
        pods = await self.k3s_client.list_pods(
            namespace="default",
            label_selector="app=verified-app"
        )

        assert len(pods) == 1
        pod = pods[0]
        assert pod.status.phase == "Running"
        assert pod.metadata.annotations.get("nautilus.io/attestation-id") == attestation_id

        # Verify attestation verification was successful
        verification_logs = await self.k3s_client.get_pod_logs(
            pod.metadata.name,
            "default",
            container="attestation-verifier"
        )

        assert "verification successful" in verification_logs.lower()

    async def _check_deployment_ready(self, deployment_name: str) -> bool:
        """Check if a deployment is ready."""
        try:
            deployment = await self.k3s_client.get_deployment(deployment_name, "default")
            if deployment is None:
                return False

            status = deployment.status
            return (status.ready_replicas is not None and
                   status.ready_replicas == deployment.spec.replicas)
        except Exception:
            return False

    @pytest.mark.asyncio
    async def test_attestation_verification_failure(self):
        """Test deployment failure when attestation verification fails."""

        # Create code without proper attestation
        unattested_code = """
        const express = require('express');
        const app = express();

        // This code has not been properly attested
        app.get('/unsafe', (req, res) => {
            res.json({ status: 'unverified', warning: 'not attested' });
        });

        app.listen(3000);
        """

        code_hash = hashlib.sha256(unattested_code.encode('utf-8')).hexdigest()

        blob_id = await self.walrus_client.store_blob(
            data=unattested_code.encode('utf-8'),
            metadata={"name": "unattested-app", "version": "1.0.0"}
        )

        # Try to deploy without attestation (should fail)
        deployment_manifest = {
            "apiVersion": "apps/v1",
            "kind": "Deployment",
            "metadata": {
                "name": "unattested-app",
                "annotations": {
                    "nautilus.io/attestation-required": "true",
                    "daas.io/walrus-blob-id": blob_id
                    # Note: No attestation-id provided
                }
            },
            "spec": {
                "replicas": 1,
                "selector": {"matchLabels": {"app": "unattested-app"}},
                "template": {
                    "metadata": {"labels": {"app": "unattested-app"}},
                    "spec": {
                        "initContainers": [{
                            "name": "attestation-verifier",
                            "image": "nautilus/verifier:latest",
                            "env": [
                                {"name": "NAUTILUS_ENDPOINT", "value": "http://nautilus-attestation:8090"},
                                {"name": "BLOB_ID", "value": blob_id},
                                {"name": "REQUIRED_COMPLIANCE", "value": "high"}
                                # Note: No ATTESTATION_ID
                            ]
                        }],
                        "containers": [{
                            "name": "app",
                            "image": "node:18-alpine",
                            "command": ["node", "/app/code.js"]
                        }]
                    }
                }
            }
        }

        # Deploy should succeed but verification should fail
        deployment = await self.k3s_client.create_deployment(
            deployment_manifest,
            namespace="default"
        )

        # Wait a bit for the verification to fail
        await asyncio.sleep(30)

        # Check that pods are not running due to failed verification
        pods = await self.k3s_client.list_pods(
            namespace="default",
            label_selector="app=unattested-app"
        )

        # Pods should exist but not be running
        assert len(pods) >= 1

        for pod in pods:
            # Should be in failed state or stuck in init
            assert pod.status.phase in ["Pending", "Failed"]

        # Verify verification failure in logs
        if pods:
            verification_logs = await self.k3s_client.get_pod_logs(
                pods[0].metadata.name,
                "default",
                container="attestation-verifier"
            )

            assert any(keyword in verification_logs.lower()
                      for keyword in ["failed", "error", "no attestation"])

    @pytest.mark.asyncio
    async def test_runtime_integrity_monitoring(self):
        """Test runtime integrity monitoring of deployed applications."""

        # Create and deploy attested application
        app_code = """
        const express = require('express');
        const fs = require('fs');
        const app = express();

        let modificationAttempts = 0;

        app.get('/health', (req, res) => {
            res.json({
                status: 'healthy',
                integrity: 'verified',
                modifications: modificationAttempts
            });
        });

        // Endpoint that simulates code modification
        app.post('/modify', (req, res) => {
            modificationAttempts++;
            // Simulate runtime modification
            fs.writeFileSync('/tmp/modified.js', 'console.log("modified");');
            res.json({ modified: true, attempts: modificationAttempts });
        });

        app.listen(3000);
        """

        code_hash = hashlib.sha256(app_code.encode('utf-8')).hexdigest()

        blob_id = await self.walrus_client.store_blob(
            data=app_code.encode('utf-8'),
            metadata={"name": "monitored-app", "version": "1.0.0"}
        )

        # Create attestation with runtime monitoring
        attestation_request = {
            "code_hash": code_hash,
            "blob_id": blob_id,
            "metadata": {
                "name": "monitored-app",
                "version": "1.0.0",
                "runtime_monitoring": True,
                "integrity_checks": True,
                "monitoring_interval": 30  # seconds
            },
            "monitoring_config": {
                "file_integrity": True,
                "process_monitoring": True,
                "network_monitoring": False,
                "alert_on_modification": True
            }
        }

        attestation_result = await self.nautilus_client.create_attestation(
            attestation_request
        )
        attestation_id = attestation_result["attestation_id"]

        # Deploy with runtime monitoring
        deployment_manifest = {
            "apiVersion": "apps/v1",
            "kind": "Deployment",
            "metadata": {
                "name": "monitored-app",
                "annotations": {
                    "nautilus.io/attestation-id": attestation_id,
                    "nautilus.io/runtime-monitoring": "true",
                    "nautilus.io/integrity-check-interval": "30s"
                }
            },
            "spec": {
                "replicas": 1,
                "selector": {"matchLabels": {"app": "monitored-app"}},
                "template": {
                    "metadata": {"labels": {"app": "monitored-app"}},
                    "spec": {
                        "containers": [
                            {
                                "name": "app",
                                "image": "node:18-alpine",
                                "command": ["node", "/app/code.js"],
                                "ports": [{"containerPort": 3000}]
                            },
                            {
                                "name": "integrity-monitor",
                                "image": "nautilus/monitor:latest",
                                "env": [
                                    {"name": "NAUTILUS_ENDPOINT", "value": "http://nautilus-attestation:8090"},
                                    {"name": "ATTESTATION_ID", "value": attestation_id},
                                    {"name": "MONITOR_INTERVAL", "value": "30"},
                                    {"name": "ALERT_ON_CHANGE", "value": "true"}
                                ],
                                "volumeMounts": [{
                                    "name": "app-files",
                                    "mountPath": "/app",
                                    "readOnly": True
                                }]
                            }
                        ],
                        "volumes": [{
                            "name": "app-files",
                            "emptyDir": {}
                        }]
                    }
                }
            }
        }

        deployment = await self.k3s_client.create_deployment(
            deployment_manifest,
            namespace="default"
        )

        # Wait for deployment to be ready
        await wait_for_condition(
            lambda: self._check_deployment_ready("monitored-app"),
            timeout=120,
            interval=10
        )

        # Start monitoring the attestation status
        monitoring_started = await self.nautilus_client.start_runtime_monitoring(
            attestation_id
        )
        assert monitoring_started["status"] == "started"

        try:
            # Wait for initial integrity baseline
            await asyncio.sleep(60)

            # Check initial integrity status
            integrity_status = await self.nautilus_client.get_integrity_status(
                attestation_id
            )

            assert integrity_status["status"] == "verified"
            assert integrity_status["violations"] == 0

            # Simulate runtime modification (this would trigger monitoring alerts)
            pods = await self.k3s_client.list_pods(
                namespace="default",
                label_selector="app=monitored-app"
            )

            if pods:
                pod_name = pods[0].metadata.name

                # Execute modification command in the pod
                modification_result = await self.k3s_client.exec_in_pod(
                    pod_name=pod_name,
                    namespace="default",
                    container="app",
                    command=["touch", "/tmp/unauthorized_file.txt"]
                )

            # Wait for monitoring to detect the modification
            await wait_for_condition(
                lambda: self._check_integrity_violation(attestation_id),
                timeout=90,
                interval=10,
                description="Waiting for integrity violation detection"
            )

            # Verify violation was detected
            final_integrity_status = await self.nautilus_client.get_integrity_status(
                attestation_id
            )

            assert final_integrity_status["status"] == "violated"
            assert final_integrity_status["violations"] > 0
            assert "unauthorized_file.txt" in str(final_integrity_status["violations_details"])

        finally:
            # Stop monitoring
            await self.nautilus_client.stop_runtime_monitoring(attestation_id)

    async def _check_integrity_violation(self, attestation_id: str) -> bool:
        """Check if an integrity violation has been detected."""
        try:
            status = await self.nautilus_client.get_integrity_status(attestation_id)
            return status["status"] == "violated" and status["violations"] > 0
        except Exception:
            return False

    @pytest.mark.asyncio
    async def test_compliance_level_enforcement(self):
        """Test enforcement of different compliance levels."""

        compliance_test_cases = [
            {
                "level": "basic",
                "requirements": {
                    "security_scan": True,
                    "vulnerability_threshold": 10,
                    "attestor_reputation": 70
                },
                "should_pass": True
            },
            {
                "level": "standard",
                "requirements": {
                    "security_scan": True,
                    "vulnerability_threshold": 5,
                    "attestor_reputation": 85,
                    "code_review": True
                },
                "should_pass": True
            },
            {
                "level": "high",
                "requirements": {
                    "security_scan": True,
                    "vulnerability_threshold": 0,
                    "attestor_reputation": 95,
                    "code_review": True,
                    "penetration_test": True
                },
                "should_pass": True
            },
            {
                "level": "critical",
                "requirements": {
                    "security_scan": True,
                    "vulnerability_threshold": 0,
                    "attestor_reputation": 98,
                    "code_review": True,
                    "penetration_test": True,
                    "formal_verification": True,
                    "multi_party_attestation": True
                },
                "should_pass": False  # Too strict for our test setup
            }
        ]

        for i, test_case in enumerate(compliance_test_cases):
            compliance_level = test_case["level"]
            requirements = test_case["requirements"]
            should_pass = test_case["should_pass"]

            # Create test application
            app_code = f"""
            const express = require('express');
            const app = express();

            app.get('/compliance', (req, res) => {{
                res.json({{
                    level: '{compliance_level}',
                    compliant: true,
                    requirements: {json.dumps(requirements)}
                }});
            }});

            app.listen(3000);
            """

            code_hash = hashlib.sha256(app_code.encode('utf-8')).hexdigest()

            blob_id = await self.walrus_client.store_blob(
                data=app_code.encode('utf-8'),
                metadata={
                    "name": f"compliance-test-{i}",
                    "version": "1.0.0",
                    "compliance_level": compliance_level
                }
            )

            # Create attestation meeting requirements
            attestation_metadata = {
                "name": f"compliance-test-{i}",
                "version": "1.0.0",
                "compliance_level": compliance_level,
                "security_scan": "passed" if requirements.get("security_scan") else "skipped",
                "vulnerability_count": min(requirements.get("vulnerability_threshold", 0), 0),
                "code_review": "completed" if requirements.get("code_review") else "skipped",
                "penetration_test": "passed" if requirements.get("penetration_test") else "skipped",
                "formal_verification": "completed" if requirements.get("formal_verification") else "skipped"
            }

            attestation_request = {
                "code_hash": code_hash,
                "blob_id": blob_id,
                "metadata": attestation_metadata,
                "attestor": {
                    "name": f"attestor-{compliance_level}",
                    "reputation_score": requirements.get("attestor_reputation", 95),
                    "certifications": ["iso27001"] if compliance_level in ["high", "critical"] else []
                },
                "compliance_validation": {
                    "level": compliance_level,
                    "enforce_requirements": True
                }
            }

            if should_pass:
                # Should succeed
                attestation_result = await self.nautilus_client.create_attestation(
                    attestation_request
                )

                assert attestation_result["status"] == "success"
                assert attestation_result["compliance_level"] == compliance_level

                # Verify compliance validation
                compliance_check = await self.nautilus_client.validate_compliance(
                    attestation_result["attestation_id"],
                    required_level=compliance_level
                )

                assert compliance_check["valid"] is True
                assert compliance_check["level"] == compliance_level

            else:
                # Should fail due to strict requirements
                with pytest.raises(Exception) as exc_info:
                    await self.nautilus_client.create_attestation(attestation_request)

                assert "compliance requirements not met" in str(exc_info.value).lower()

    @pytest.mark.asyncio
    async def test_multi_attestor_consensus(self):
        """Test multi-attestor consensus for high-security deployments."""

        # Create application requiring multiple attestations
        critical_app_code = """
        const express = require('express');
        const crypto = require('crypto');

        const app = express();

        app.get('/secure', (req, res) => {
            const token = crypto.randomBytes(32).toString('hex');
            res.json({
                message: 'Critical security application',
                token: token,
                multi_attested: true
            });
        });

        app.listen(3000);
        """

        code_hash = hashlib.sha256(critical_app_code.encode('utf-8')).hexdigest()

        blob_id = await self.walrus_client.store_blob(
            data=critical_app_code.encode('utf-8'),
            metadata={
                "name": "critical-app",
                "version": "1.0.0",
                "classification": "critical"
            }
        )

        # Create multiple attestations from different attestors
        attestors = [
            {
                "name": "security-attestor",
                "reputation": 98,
                "specialty": "security",
                "public_key": "0x1111111111111111111111111111111111111111"
            },
            {
                "name": "compliance-attestor",
                "reputation": 96,
                "specialty": "compliance",
                "public_key": "0x2222222222222222222222222222222222222222"
            },
            {
                "name": "performance-attestor",
                "reputation": 94,
                "specialty": "performance",
                "public_key": "0x3333333333333333333333333333333333333333"
            }
        ]

        attestation_ids = []

        for attestor in attestors:
            attestation_request = {
                "code_hash": code_hash,
                "blob_id": blob_id,
                "metadata": {
                    "name": "critical-app",
                    "version": "1.0.0",
                    "attestor_specialty": attestor["specialty"],
                    "security_scan": "passed",
                    "compliance_check": "passed",
                    "performance_test": "passed"
                },
                "attestor": {
                    "name": attestor["name"],
                    "public_key": attestor["public_key"],
                    "reputation_score": attestor["reputation"],
                    "specialty": attestor["specialty"]
                },
                "multi_attestor_group": "critical-app-group"
            }

            result = await self.nautilus_client.create_attestation(attestation_request)
            attestation_ids.append(result["attestation_id"])

        # Create consensus attestation requiring all three
        consensus_result = await self.nautilus_client.create_consensus_attestation(
            attestation_ids=attestation_ids,
            consensus_requirements={
                "minimum_attestors": 3,
                "required_specialties": ["security", "compliance", "performance"],
                "minimum_reputation": 90,
                "consensus_threshold": 1.0  # 100% agreement required
            }
        )

        assert consensus_result["status"] == "success"
        assert consensus_result["consensus_achieved"] is True
        assert len(consensus_result["participating_attestors"]) == 3

        consensus_attestation_id = consensus_result["consensus_attestation_id"]

        # Deploy with consensus requirement
        deployment_manifest = {
            "apiVersion": "apps/v1",
            "kind": "Deployment",
            "metadata": {
                "name": "critical-app",
                "annotations": {
                    "nautilus.io/consensus-attestation-id": consensus_attestation_id,
                    "nautilus.io/multi-attestor-required": "true",
                    "nautilus.io/minimum-attestors": "3",
                    "nautilus.io/compliance-level": "critical"
                }
            },
            "spec": {
                "replicas": 1,
                "selector": {"matchLabels": {"app": "critical-app"}},
                "template": {
                    "metadata": {"labels": {"app": "critical-app"}},
                    "spec": {
                        "initContainers": [{
                            "name": "consensus-verifier",
                            "image": "nautilus/consensus-verifier:latest",
                            "env": [
                                {"name": "NAUTILUS_ENDPOINT", "value": "http://nautilus-attestation:8090"},
                                {"name": "CONSENSUS_ATTESTATION_ID", "value": consensus_attestation_id},
                                {"name": "REQUIRED_ATTESTORS", "value": "3"},
                                {"name": "VERIFY_CONSENSUS", "value": "true"}
                            ]
                        }],
                        "containers": [{
                            "name": "app",
                            "image": "node:18-alpine",
                            "command": ["node", "/app/code.js"]
                        }]
                    }
                }
            }
        }

        deployment = await self.k3s_client.create_deployment(
            deployment_manifest,
            namespace="default"
        )

        # Wait for consensus verification and deployment
        await wait_for_condition(
            lambda: self._check_deployment_ready("critical-app"),
            timeout=180,
            interval=15,
            description="Waiting for consensus-attested deployment"
        )

        # Verify consensus attestation is working
        consensus_status = await self.nautilus_client.get_consensus_status(
            consensus_attestation_id
        )

        assert consensus_status["active"] is True
        assert consensus_status["consensus_maintained"] is True
        assert len(consensus_status["active_attestors"]) == 3

        # Verify deployment is running with consensus attestation
        pods = await self.k3s_client.list_pods(
            namespace="default",
            label_selector="app=critical-app"
        )

        assert len(pods) == 1
        assert pods[0].status.phase == "Running"