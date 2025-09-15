"""
Walrus Code Deployment Integration Tests

Tests decentralized code deployment through Walrus including:
- Blob storage and retrieval
- Code deployment from Walrus to K3s
- Attestation verification
- Version management
"""

import pytest
import asyncio
import hashlib
import json
import base64
from pathlib import Path

from .utils.walrus_client import WalrusTestClient
from .utils.k3s_client import K3sClient
from .utils.daas_client import DaaSClient
from .utils.test_helpers import wait_for_condition, retry_async


class TestWalrusDeployment:
    """Test suite for Walrus-based code deployment."""

    @pytest.fixture(autouse=True)
    async def setup_test_environment(self):
        """Set up test environment before each test."""
        self.walrus_client = WalrusTestClient()
        self.k3s_client = K3sClient()
        self.daas_client = DaaSClient()

        # Ensure Walrus simulator is ready
        await wait_for_condition(
            lambda: self.walrus_client.is_healthy(),
            timeout=60,
            interval=5,
            description="Waiting for Walrus simulator to be ready"
        )

        yield

        # Cleanup test blobs and deployments
        await self._cleanup_test_resources()

    async def _cleanup_test_resources(self):
        """Clean up test resources after each test."""
        try:
            # Clean up test deployments
            deployments = await self.k3s_client.list_deployments(
                namespace="default",
                label_selector="daas.io/test=true"
            )
            for deployment in deployments:
                await self.k3s_client.delete_deployment(
                    deployment.metadata.name,
                    "default"
                )

            # Clean up test blobs from Walrus
            await self.walrus_client.cleanup_test_blobs()

        except Exception as e:
            print(f"Warning: Failed to cleanup test resources: {e}")

    @pytest.mark.asyncio
    async def test_simple_blob_storage_and_retrieval(self):
        """Test basic blob storage and retrieval functionality."""

        # Test data
        test_code = """
        const express = require('express');
        const app = express();
        const port = 3000;

        app.get('/health', (req, res) => {
            res.json({ status: 'healthy', version: '1.0.0' });
        });

        app.listen(port, () => {
            console.log('Test app listening on port', port);
        });
        """

        test_metadata = {
            "name": "test-app",
            "version": "1.0.0",
            "runtime": "nodejs",
            "entrypoint": "app.js"
        }

        # Store blob in Walrus
        blob_id = await self.walrus_client.store_blob(
            data=test_code.encode('utf-8'),
            metadata=test_metadata
        )

        assert blob_id is not None
        assert len(blob_id) > 0

        # Retrieve blob from Walrus
        retrieved_data = await self.walrus_client.get_blob(blob_id)
        retrieved_code = retrieved_data.decode('utf-8')

        assert retrieved_code == test_code

        # Verify metadata
        blob_info = await self.walrus_client.get_blob_info(blob_id)
        assert blob_info["metadata"]["name"] == "test-app"
        assert blob_info["metadata"]["version"] == "1.0.0"
        assert blob_info["metadata"]["runtime"] == "nodejs"

    @pytest.mark.asyncio
    async def test_docker_image_deployment_from_walrus(self):
        """Test deployment of containerized applications from Walrus."""

        # Create test application files
        app_files = {
            "app.js": """
                const express = require('express');
                const app = express();
                app.get('/health', (req, res) => {
                    res.json({
                        status: 'healthy',
                        source: 'walrus',
                        timestamp: new Date().toISOString()
                    });
                });
                app.listen(3000, () => console.log('App started'));
            """,
            "package.json": json.dumps({
                "name": "walrus-test-app",
                "version": "1.0.0",
                "dependencies": {"express": "^4.18.0"},
                "scripts": {"start": "node app.js"}
            }),
            "Dockerfile": """
                FROM node:18-alpine
                WORKDIR /app
                COPY package.json .
                RUN npm install
                COPY app.js .
                EXPOSE 3000
                CMD ["npm", "start"]
            """
        }

        # Store application archive in Walrus
        app_archive = await self._create_tar_archive(app_files)
        app_metadata = {
            "name": "walrus-test-app",
            "version": "1.0.0",
            "type": "docker-app",
            "files": list(app_files.keys())
        }

        blob_id = await self.walrus_client.store_blob(
            data=app_archive,
            metadata=app_metadata
        )

        # Create Kubernetes deployment that references Walrus blob
        deployment_manifest = {
            "apiVersion": "apps/v1",
            "kind": "Deployment",
            "metadata": {
                "name": "walrus-test-deployment",
                "labels": {
                    "app": "walrus-test",
                    "daas.io/test": "true"
                },
                "annotations": {
                    "daas.io/walrus-blob-id": blob_id,
                    "daas.io/source": "walrus",
                    "daas.io/version": "1.0.0"
                }
            },
            "spec": {
                "replicas": 1,
                "selector": {"matchLabels": {"app": "walrus-test"}},
                "template": {
                    "metadata": {
                        "labels": {"app": "walrus-test"},
                        "annotations": {
                            "daas.io/walrus-blob-id": blob_id
                        }
                    },
                    "spec": {
                        "initContainers": [{
                            "name": "walrus-fetcher",
                            "image": "walrus/fetcher:latest",
                            "env": [
                                {"name": "WALRUS_ENDPOINT", "value": "http://walrus-simulator:31415"},
                                {"name": "BLOB_ID", "value": blob_id},
                                {"name": "OUTPUT_PATH", "value": "/shared/app"}
                            ],
                            "volumeMounts": [{
                                "name": "shared-storage",
                                "mountPath": "/shared"
                            }]
                        }],
                        "containers": [{
                            "name": "app",
                            "image": "docker:dind",
                            "command": ["/bin/sh", "-c"],
                            "args": [
                                "cd /shared/app && docker build -t walrus-test . && docker run -p 3000:3000 walrus-test"
                            ],
                            "volumeMounts": [{
                                "name": "shared-storage",
                                "mountPath": "/shared"
                            }],
                            "ports": [{"containerPort": 3000}]
                        }],
                        "volumes": [{
                            "name": "shared-storage",
                            "emptyDir": {}
                        }]
                    }
                }
            }
        }

        # Deploy to K3s
        deployment = await self.k3s_client.create_deployment(
            deployment_manifest,
            namespace="default"
        )

        assert deployment is not None
        assert deployment.metadata.name == "walrus-test-deployment"

        # Wait for deployment to be ready
        await wait_for_condition(
            lambda: self._check_deployment_ready("walrus-test-deployment"),
            timeout=300,  # Allow time for image building
            interval=10,
            description="Waiting for Walrus deployment to be ready"
        )

        # Verify deployment is running and accessible
        pods = await self.k3s_client.list_pods(
            namespace="default",
            label_selector="app=walrus-test"
        )

        assert len(pods) == 1
        pod = pods[0]
        assert pod.status.phase == "Running"

        # Verify annotation with blob ID
        assert pod.metadata.annotations.get("daas.io/walrus-blob-id") == blob_id

    async def _create_tar_archive(self, files: dict) -> bytes:
        """Create a tar archive from a dictionary of files."""
        import tarfile
        import io

        tar_buffer = io.BytesIO()
        with tarfile.open(fileobj=tar_buffer, mode='w:gz') as tar:
            for filename, content in files.items():
                file_data = content.encode('utf-8') if isinstance(content, str) else content
                tarinfo = tarfile.TarInfo(name=filename)
                tarinfo.size = len(file_data)
                tar.addfile(tarinfo, io.BytesIO(file_data))

        return tar_buffer.getvalue()

    async def _check_deployment_ready(self, deployment_name: str) -> bool:
        """Check if a deployment is ready."""
        try:
            deployment = await self.k3s_client.get_deployment(deployment_name, "default")
            if deployment is None:
                return False

            # Check if all replicas are ready
            status = deployment.status
            return (status.ready_replicas is not None and
                   status.ready_replicas == deployment.spec.replicas)
        except Exception:
            return False

    @pytest.mark.asyncio
    async def test_configuration_deployment_from_walrus(self):
        """Test deployment of configuration files from Walrus."""

        # Create test configuration
        config_data = {
            "database": {
                "host": "postgres.example.com",
                "port": 5432,
                "name": "testdb"
            },
            "redis": {
                "host": "redis.example.com",
                "port": 6379
            },
            "features": {
                "feature_a": True,
                "feature_b": False
            }
        }

        config_yaml = """
apiVersion: v1
kind: ConfigMap
metadata:
  name: app-config
  labels:
    daas.io/source: walrus
data:
  config.json: |
    """ + json.dumps(config_data, indent=2)

        # Store configuration in Walrus
        config_metadata = {
            "name": "app-config",
            "type": "kubernetes-config",
            "version": "1.0.0"
        }

        blob_id = await self.walrus_client.store_blob(
            data=config_yaml.encode('utf-8'),
            metadata=config_metadata
        )

        # Deploy configuration using DaaS client
        deployment_result = await self.daas_client.deploy_from_walrus(
            blob_id=blob_id,
            namespace="default",
            deployment_type="configmap"
        )

        assert deployment_result["status"] == "success"
        assert deployment_result["resource_type"] == "ConfigMap"

        # Verify ConfigMap was created
        configmap = await self.k3s_client.get_configmap("app-config", "default")
        assert configmap is not None
        assert "config.json" in configmap.data

        # Verify configuration content
        config_content = json.loads(configmap.data["config.json"])
        assert config_content["database"]["host"] == "postgres.example.com"
        assert config_content["features"]["feature_a"] is True

    @pytest.mark.asyncio
    async def test_multi_file_application_deployment(self):
        """Test deployment of complex multi-file applications."""

        # Create a more complex application
        app_structure = {
            "src/index.js": """
                const express = require('express');
                const config = require('./config');
                const routes = require('./routes');

                const app = express();
                app.use('/api', routes);
                app.listen(config.port, () => {
                    console.log('Server running on port', config.port);
                });
            """,
            "src/config.js": """
                module.exports = {
                    port: process.env.PORT || 3000,
                    database: process.env.DATABASE_URL || 'sqlite:memory',
                    features: {
                        logging: true,
                        metrics: true
                    }
                };
            """,
            "src/routes.js": """
                const express = require('express');
                const router = express.Router();

                router.get('/health', (req, res) => {
                    res.json({ status: 'healthy', version: '2.0.0' });
                });

                router.get('/info', (req, res) => {
                    res.json({
                        name: 'Multi-file App',
                        source: 'walrus',
                        files: ['index.js', 'config.js', 'routes.js']
                    });
                });

                module.exports = router;
            """,
            "package.json": json.dumps({
                "name": "multi-file-app",
                "version": "2.0.0",
                "main": "src/index.js",
                "dependencies": {
                    "express": "^4.18.0"
                },
                "scripts": {
                    "start": "node src/index.js"
                }
            }),
            "Dockerfile": """
                FROM node:18-alpine
                WORKDIR /app
                COPY package.json .
                RUN npm install
                COPY src/ ./src/
                EXPOSE 3000
                CMD ["npm", "start"]
            """,
            ".dockerignore": """
                node_modules
                .git
                .gitignore
                README.md
            """
        }

        # Store application bundle
        app_archive = await self._create_tar_archive(app_structure)
        app_metadata = {
            "name": "multi-file-app",
            "version": "2.0.0",
            "type": "nodejs-app",
            "structure": "multi-file",
            "files": list(app_structure.keys())
        }

        blob_id = await self.walrus_client.store_blob(
            data=app_archive,
            metadata=app_metadata
        )

        # Deploy using advanced DaaS deployment
        deployment_config = {
            "name": "multi-file-app",
            "namespace": "default",
            "replicas": 2,
            "resources": {
                "requests": {"memory": "128Mi", "cpu": "100m"},
                "limits": {"memory": "256Mi", "cpu": "200m"}
            },
            "env": [
                {"name": "NODE_ENV", "value": "production"},
                {"name": "PORT", "value": "3000"}
            ],
            "annotations": {
                "daas.io/walrus-blob-id": blob_id,
                "daas.io/multi-file": "true"
            }
        }

        deployment_result = await self.daas_client.deploy_application_from_walrus(
            blob_id=blob_id,
            config=deployment_config
        )

        assert deployment_result["status"] == "success"
        assert deployment_result["deployment_name"] == "multi-file-app"

        # Wait for deployment
        await wait_for_condition(
            lambda: self._check_deployment_ready("multi-file-app"),
            timeout=300,
            interval=10
        )

        # Verify multiple replicas
        pods = await self.k3s_client.list_pods(
            namespace="default",
            label_selector="app=multi-file-app"
        )

        assert len(pods) == 2  # Should have 2 replicas
        for pod in pods:
            assert pod.status.phase == "Running"
            assert pod.metadata.annotations.get("daas.io/walrus-blob-id") == blob_id

    @pytest.mark.asyncio
    async def test_deployment_version_management(self):
        """Test version management and rollback capabilities."""

        app_name = "versioned-app"

        # Deploy version 1.0.0
        v1_code = """
        const express = require('express');
        const app = express();
        app.get('/version', (req, res) => {
            res.json({ version: '1.0.0', features: ['basic'] });
        });
        app.listen(3000);
        """

        v1_files = {
            "app.js": v1_code,
            "package.json": json.dumps({"name": app_name, "version": "1.0.0"})
        }

        v1_blob_id = await self.walrus_client.store_blob(
            data=await self._create_tar_archive(v1_files),
            metadata={"name": app_name, "version": "1.0.0"}
        )

        # Deploy v1.0.0
        await self.daas_client.deploy_application_from_walrus(
            blob_id=v1_blob_id,
            config={"name": app_name, "namespace": "default", "version": "1.0.0"}
        )

        await wait_for_condition(
            lambda: self._check_deployment_ready(app_name),
            timeout=120,
            interval=5
        )

        # Verify v1.0.0 is running
        deployment_v1 = await self.k3s_client.get_deployment(app_name, "default")
        assert deployment_v1.metadata.annotations.get("daas.io/walrus-blob-id") == v1_blob_id

        # Deploy version 2.0.0
        v2_code = """
        const express = require('express');
        const app = express();
        app.get('/version', (req, res) => {
            res.json({ version: '2.0.0', features: ['basic', 'advanced'] });
        });
        app.get('/health', (req, res) => {
            res.json({ status: 'healthy', version: '2.0.0' });
        });
        app.listen(3000);
        """

        v2_files = {
            "app.js": v2_code,
            "package.json": json.dumps({"name": app_name, "version": "2.0.0"})
        }

        v2_blob_id = await self.walrus_client.store_blob(
            data=await self._create_tar_archive(v2_files),
            metadata={"name": app_name, "version": "2.0.0"}
        )

        # Update deployment to v2.0.0
        update_result = await self.daas_client.update_application_from_walrus(
            app_name=app_name,
            blob_id=v2_blob_id,
            namespace="default",
            version="2.0.0"
        )

        assert update_result["status"] == "success"
        assert update_result["previous_version"] == "1.0.0"
        assert update_result["new_version"] == "2.0.0"

        # Wait for rollout to complete
        await wait_for_condition(
            lambda: self._check_deployment_version(app_name, v2_blob_id),
            timeout=120,
            interval=5
        )

        # Test rollback to v1.0.0
        rollback_result = await self.daas_client.rollback_application(
            app_name=app_name,
            target_blob_id=v1_blob_id,
            namespace="default"
        )

        assert rollback_result["status"] == "success"
        assert rollback_result["target_version"] == "1.0.0"

        # Wait for rollback to complete
        await wait_for_condition(
            lambda: self._check_deployment_version(app_name, v1_blob_id),
            timeout=120,
            interval=5
        )

        # Verify we're back to v1.0.0
        deployment_rolled_back = await self.k3s_client.get_deployment(app_name, "default")
        assert deployment_rolled_back.metadata.annotations.get("daas.io/walrus-blob-id") == v1_blob_id

    async def _check_deployment_version(self, app_name: str, expected_blob_id: str) -> bool:
        """Check if deployment is using the expected blob ID."""
        try:
            deployment = await self.k3s_client.get_deployment(app_name, "default")
            if deployment is None:
                return False

            current_blob_id = deployment.metadata.annotations.get("daas.io/walrus-blob-id")
            return current_blob_id == expected_blob_id
        except Exception:
            return False

    @pytest.mark.asyncio
    async def test_large_blob_deployment(self):
        """Test deployment of large application bundles."""

        # Create a large application with many files
        large_app_files = {}

        # Generate multiple modules
        for i in range(50):
            module_code = f"""
            module.exports = {{
                name: 'module_{i:03d}',
                version: '1.0.0',
                execute: function() {{
                    console.log('Executing module {i:03d}');
                    return 'result_{i:03d}';
                }}
            }};
            """
            large_app_files[f"modules/module_{i:03d}.js"] = module_code

        # Main application file
        large_app_files["app.js"] = """
        const express = require('express');
        const fs = require('fs');
        const path = require('path');

        const app = express();

        // Load all modules
        const modules = {};
        const modulesDir = path.join(__dirname, 'modules');
        fs.readdirSync(modulesDir).forEach(file => {
            if (file.endsWith('.js')) {
                const moduleName = file.replace('.js', '');
                modules[moduleName] = require(path.join(modulesDir, file));
            }
        });

        app.get('/modules', (req, res) => {
            res.json({
                count: Object.keys(modules).length,
                modules: Object.keys(modules)
            });
        });

        app.listen(3000);
        """

        large_app_files["package.json"] = json.dumps({
            "name": "large-app",
            "version": "1.0.0",
            "main": "app.js"
        })

        # Create large blob (should be several MB)
        large_archive = await self._create_tar_archive(large_app_files)

        # Verify blob is reasonably large
        assert len(large_archive) > 1024 * 1024  # At least 1MB

        blob_metadata = {
            "name": "large-app",
            "version": "1.0.0",
            "type": "large-nodejs-app",
            "file_count": len(large_app_files),
            "size_bytes": len(large_archive)
        }

        # Store large blob
        blob_id = await self.walrus_client.store_blob(
            data=large_archive,
            metadata=blob_metadata,
            chunk_size=1024*1024  # 1MB chunks
        )

        assert blob_id is not None

        # Deploy large application
        deployment_result = await self.daas_client.deploy_application_from_walrus(
            blob_id=blob_id,
            config={
                "name": "large-app",
                "namespace": "default",
                "resources": {
                    "requests": {"memory": "512Mi", "cpu": "200m"},
                    "limits": {"memory": "1Gi", "cpu": "500m"}
                }
            },
            timeout=600  # 10 minutes for large deployment
        )

        assert deployment_result["status"] == "success"

        # Wait for large deployment
        await wait_for_condition(
            lambda: self._check_deployment_ready("large-app"),
            timeout=600,  # 10 minutes
            interval=15
        )

        # Verify deployment is working
        deployment = await self.k3s_client.get_deployment("large-app", "default")
        assert deployment is not None
        assert deployment.metadata.annotations.get("daas.io/walrus-blob-id") == blob_id