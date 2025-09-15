"""
K3s Client for integration testing.
Provides interface for Kubernetes operations.
"""

import asyncio
import json
from typing import Dict, Any, List, Optional
from kubernetes import client, config
from kubernetes.client.rest import ApiException


class K3sClient:
    """Client for K3s cluster operations during testing."""

    def __init__(self):
        self.v1 = None
        self.apps_v1 = None
        self._initialize_client()

    def _initialize_client(self):
        """Initialize Kubernetes client."""
        try:
            # Try to load in-cluster config first
            config.load_incluster_config()
        except:
            try:
                # Fall back to kubeconfig
                config.load_kube_config()
            except:
                # For testing, we'll mock the client
                pass

        self.v1 = client.CoreV1Api()
        self.apps_v1 = client.AppsV1Api()

    async def list_nodes(self) -> List[Any]:
        """
        List all nodes in the cluster.

        Returns:
            List of node objects
        """
        try:
            response = self.v1.list_node()
            return response.items
        except ApiException as e:
            if e.status == 404:
                return []
            raise

    async def get_node(self, node_name: str) -> Optional[Any]:
        """
        Get a specific node by name.

        Args:
            node_name: Name of the node

        Returns:
            Node object or None if not found
        """
        try:
            return self.v1.read_node(name=node_name)
        except ApiException as e:
            if e.status == 404:
                return None
            raise

    async def delete_node(self, node_name: str):
        """
        Delete a node from the cluster.

        Args:
            node_name: Name of the node to delete
        """
        try:
            self.v1.delete_node(name=node_name)
        except ApiException as e:
            if e.status != 404:  # Ignore if already deleted
                raise

    async def list_deployments(
        self,
        namespace: str = "default",
        label_selector: Optional[str] = None
    ) -> List[Any]:
        """
        List deployments in a namespace.

        Args:
            namespace: Kubernetes namespace
            label_selector: Optional label selector

        Returns:
            List of deployment objects
        """
        try:
            response = self.apps_v1.list_namespaced_deployment(
                namespace=namespace,
                label_selector=label_selector
            )
            return response.items
        except ApiException as e:
            if e.status == 404:
                return []
            raise

    async def get_deployment(
        self,
        deployment_name: str,
        namespace: str = "default"
    ) -> Optional[Any]:
        """
        Get a specific deployment.

        Args:
            deployment_name: Name of the deployment
            namespace: Kubernetes namespace

        Returns:
            Deployment object or None if not found
        """
        try:
            return self.apps_v1.read_namespaced_deployment(
                name=deployment_name,
                namespace=namespace
            )
        except ApiException as e:
            if e.status == 404:
                return None
            raise

    async def create_deployment(
        self,
        deployment_manifest: Dict[str, Any],
        namespace: str = "default"
    ) -> Any:
        """
        Create a new deployment.

        Args:
            deployment_manifest: Deployment manifest
            namespace: Kubernetes namespace

        Returns:
            Created deployment object
        """
        # Convert dict to V1Deployment object
        deployment = client.V1Deployment(**deployment_manifest)

        return self.apps_v1.create_namespaced_deployment(
            namespace=namespace,
            body=deployment
        )

    async def delete_deployment(
        self,
        deployment_name: str,
        namespace: str = "default"
    ):
        """
        Delete a deployment.

        Args:
            deployment_name: Name of the deployment
            namespace: Kubernetes namespace
        """
        try:
            self.apps_v1.delete_namespaced_deployment(
                name=deployment_name,
                namespace=namespace
            )
        except ApiException as e:
            if e.status != 404:  # Ignore if already deleted
                raise

    async def list_pods(
        self,
        namespace: str = "default",
        label_selector: Optional[str] = None
    ) -> List[Any]:
        """
        List pods in a namespace.

        Args:
            namespace: Kubernetes namespace
            label_selector: Optional label selector

        Returns:
            List of pod objects
        """
        try:
            response = self.v1.list_namespaced_pod(
                namespace=namespace,
                label_selector=label_selector
            )
            return response.items
        except ApiException as e:
            if e.status == 404:
                return []
            raise

    async def get_pod(
        self,
        pod_name: str,
        namespace: str = "default"
    ) -> Optional[Any]:
        """
        Get a specific pod.

        Args:
            pod_name: Name of the pod
            namespace: Kubernetes namespace

        Returns:
            Pod object or None if not found
        """
        try:
            return self.v1.read_namespaced_pod(
                name=pod_name,
                namespace=namespace
            )
        except ApiException as e:
            if e.status == 404:
                return None
            raise

    async def get_pod_logs(
        self,
        pod_name: str,
        namespace: str = "default",
        container: Optional[str] = None,
        tail_lines: int = 100
    ) -> str:
        """
        Get logs from a pod.

        Args:
            pod_name: Name of the pod
            namespace: Kubernetes namespace
            container: Optional container name
            tail_lines: Number of lines to tail

        Returns:
            Pod logs as string
        """
        try:
            return self.v1.read_namespaced_pod_log(
                name=pod_name,
                namespace=namespace,
                container=container,
                tail_lines=tail_lines
            )
        except ApiException as e:
            if e.status == 404:
                return ""
            raise

    async def exec_in_pod(
        self,
        pod_name: str,
        namespace: str = "default",
        container: Optional[str] = None,
        command: List[str] = None
    ) -> str:
        """
        Execute command in a pod.

        Args:
            pod_name: Name of the pod
            namespace: Kubernetes namespace
            container: Optional container name
            command: Command to execute

        Returns:
            Command output
        """
        from kubernetes.stream import stream

        if command is None:
            command = ["/bin/sh"]

        try:
            resp = stream(
                self.v1.connect_get_namespaced_pod_exec,
                pod_name,
                namespace,
                container=container,
                command=command,
                stderr=True,
                stdin=False,
                stdout=True,
                tty=False
            )
            return resp
        except ApiException as e:
            if e.status == 404:
                return ""
            raise

    async def get_configmap(
        self,
        configmap_name: str,
        namespace: str = "default"
    ) -> Optional[Any]:
        """
        Get a ConfigMap.

        Args:
            configmap_name: Name of the ConfigMap
            namespace: Kubernetes namespace

        Returns:
            ConfigMap object or None if not found
        """
        try:
            return self.v1.read_namespaced_config_map(
                name=configmap_name,
                namespace=namespace
            )
        except ApiException as e:
            if e.status == 404:
                return None
            raise

    async def create_configmap(
        self,
        configmap_manifest: Dict[str, Any],
        namespace: str = "default"
    ) -> Any:
        """
        Create a ConfigMap.

        Args:
            configmap_manifest: ConfigMap manifest
            namespace: Kubernetes namespace

        Returns:
            Created ConfigMap object
        """
        configmap = client.V1ConfigMap(**configmap_manifest)

        return self.v1.create_namespaced_config_map(
            namespace=namespace,
            body=configmap
        )

    async def create_secret(
        self,
        secret_manifest: Dict[str, Any],
        namespace: str = "default"
    ) -> Any:
        """
        Create a Secret.

        Args:
            secret_manifest: Secret manifest
            namespace: Kubernetes namespace

        Returns:
            Created Secret object
        """
        secret = client.V1Secret(**secret_manifest)

        return self.v1.create_namespaced_secret(
            namespace=namespace,
            body=secret
        )

    async def apply_manifest(
        self,
        manifest: Dict[str, Any],
        namespace: str = "default"
    ) -> Any:
        """
        Apply a Kubernetes manifest.

        Args:
            manifest: Kubernetes manifest
            namespace: Kubernetes namespace

        Returns:
            Created/updated resource
        """
        kind = manifest.get("kind", "").lower()

        if kind == "deployment":
            return await self.create_deployment(manifest, namespace)
        elif kind == "configmap":
            return await self.create_configmap(manifest, namespace)
        elif kind == "secret":
            return await self.create_secret(manifest, namespace)
        else:
            raise ValueError(f"Unsupported manifest kind: {kind}")

    async def wait_for_deployment_ready(
        self,
        deployment_name: str,
        namespace: str = "default",
        timeout: int = 300
    ) -> bool:
        """
        Wait for a deployment to be ready.

        Args:
            deployment_name: Name of the deployment
            namespace: Kubernetes namespace
            timeout: Timeout in seconds

        Returns:
            True if deployment becomes ready
        """
        start_time = asyncio.get_event_loop().time()

        while asyncio.get_event_loop().time() - start_time < timeout:
            try:
                deployment = await self.get_deployment(deployment_name, namespace)

                if deployment and deployment.status:
                    if (deployment.status.ready_replicas is not None and
                        deployment.status.ready_replicas == deployment.spec.replicas):
                        return True

                await asyncio.sleep(5)

            except Exception:
                await asyncio.sleep(5)

        return False

    async def wait_for_pod_ready(
        self,
        pod_name: str,
        namespace: str = "default",
        timeout: int = 300
    ) -> bool:
        """
        Wait for a pod to be ready.

        Args:
            pod_name: Name of the pod
            namespace: Kubernetes namespace
            timeout: Timeout in seconds

        Returns:
            True if pod becomes ready
        """
        start_time = asyncio.get_event_loop().time()

        while asyncio.get_event_loop().time() - start_time < timeout:
            try:
                pod = await self.get_pod(pod_name, namespace)

                if pod and pod.status:
                    if pod.status.phase == "Running":
                        # Check if all containers are ready
                        if pod.status.container_statuses:
                            all_ready = all(
                                container.ready for container in pod.status.container_statuses
                            )
                            if all_ready:
                                return True

                await asyncio.sleep(5)

            except Exception:
                await asyncio.sleep(5)

        return False

    async def get_cluster_info(self) -> Dict[str, Any]:
        """
        Get cluster information.

        Returns:
            Dict with cluster information
        """
        try:
            nodes = await self.list_nodes()
            deployments = await self.list_deployments()

            return {
                "node_count": len(nodes),
                "deployment_count": len(deployments),
                "cluster_ready": len(nodes) > 0
            }
        except Exception as e:
            return {
                "error": str(e),
                "cluster_ready": False
            }

    async def cleanup_namespace(self, namespace: str = "default"):
        """
        Clean up all resources in a namespace.

        Args:
            namespace: Kubernetes namespace to clean up
        """
        try:
            # Delete all deployments
            deployments = await self.list_deployments(namespace)
            for deployment in deployments:
                await self.delete_deployment(deployment.metadata.name, namespace)

            # Delete all ConfigMaps
            configmaps = self.v1.list_namespaced_config_map(namespace=namespace)
            for configmap in configmaps.items:
                if not configmap.metadata.name.startswith("kube-"):  # Skip system ConfigMaps
                    self.v1.delete_namespaced_config_map(
                        name=configmap.metadata.name,
                        namespace=namespace
                    )

            # Delete all Secrets
            secrets = self.v1.list_namespaced_secret(namespace=namespace)
            for secret in secrets.items:
                if secret.type != "kubernetes.io/service-account-token":  # Skip SA tokens
                    self.v1.delete_namespaced_secret(
                        name=secret.metadata.name,
                        namespace=namespace
                    )

        except Exception as e:
            print(f"Warning: Failed to cleanup namespace {namespace}: {e}")