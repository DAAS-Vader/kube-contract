/**
 * Nautilus TEE Worker Node Communication System
 * Real-time WebSocket communication between worker nodes and TEE master
 * Supports high-frequency updates, command dispatch, and reliable connection management
 */

const WebSocket = require('ws');
const EventEmitter = require('events');

/**
 * TEE Master WebSocket Server
 * Handles connections from multiple worker nodes
 */
class TEEMasterServer extends EventEmitter {
    constructor(options = {}) {
        super();

        this.config = {
            port: options.port || 8080,
            maxConnections: options.maxConnections || 1000,
            heartbeatInterval: options.heartbeatInterval || 30000, // 30 seconds
            updateInterval: options.updateInterval || 1000, // 1 second
            reconnectTimeout: options.reconnectTimeout || 5000,
            maxMessageSize: options.maxMessageSize || 1024 * 1024, // 1MB
            enableCompression: options.enableCompression !== false,
            ...options
        };

        this.server = null;
        this.workers = new Map(); // nodeId -> WorkerConnection
        this.connectionPool = new ConnectionPool(this.config.maxConnections);
        this.messageQueue = new PriorityMessageQueue();
        this.metrics = new CommunicationMetrics();

        // Message handlers
        this.messageHandlers = new Map([
            ['STATE_UPDATE', this.handleStateUpdate.bind(this)],
            ['HEARTBEAT', this.handleHeartbeat.bind(this)],
            ['POD_STATUS', this.handlePodStatus.bind(this)],
            ['NODE_STATUS', this.handleNodeStatus.bind(this)],
            ['ERROR_REPORT', this.handleErrorReport.bind(this)],
            ['RESOURCE_METRICS', this.handleResourceMetrics.bind(this)]
        ]);

        // Command types for worker nodes
        this.commandTypes = {
            CREATE_POD: 'CREATE_POD',
            DELETE_POD: 'DELETE_POD',
            UPDATE_POD: 'UPDATE_POD',
            SCALE_DEPLOYMENT: 'SCALE_DEPLOYMENT',
            EXECUTE_COMMAND: 'EXECUTE_COMMAND',
            UPDATE_CONFIG: 'UPDATE_CONFIG',
            COLLECT_LOGS: 'COLLECT_LOGS',
            HEALTH_CHECK: 'HEALTH_CHECK'
        };

        this.initializeServer();
    }

    /**
     * Initialize WebSocket server
     */
    initializeServer() {
        this.server = new WebSocket.Server({
            port: this.config.port,
            perMessageDeflate: this.config.enableCompression,
            maxPayload: this.config.maxMessageSize,
            clientTracking: true
        });

        this.server.on('connection', this.handleConnection.bind(this));
        this.server.on('error', this.handleServerError.bind(this));

        // Start background workers
        this.startHeartbeatWorker();
        this.startMessageProcessor();
        this.startMetricsCollector();

        console.log(`TEE Master WebSocket server listening on port ${this.config.port}`);
    }

    /**
     * Handle new worker connection
     * @param {WebSocket} ws
     * @param {Object} request
     */
    handleConnection(ws, request) {
        console.log(`New worker connection from ${request.socket.remoteAddress}`);

        // Set up connection wrapper
        const connection = new WorkerConnection(ws, this.config);

        ws.on('message', (data) => {
            this.handleMessage(connection, data);
        });

        ws.on('close', (code, reason) => {
            this.handleDisconnection(connection, code, reason);
        });

        ws.on('error', (error) => {
            this.handleConnectionError(connection, error);
        });

        ws.on('pong', () => {
            connection.updateLastPong();
        });

        // Add to connection pool
        this.connectionPool.addConnection(connection);
        this.metrics.recordConnection();
    }

    /**
     * Handle incoming message from worker
     * @param {WorkerConnection} connection
     * @param {Buffer} data
     */
    async handleMessage(connection, data) {
        const startTime = Date.now();

        try {
            const message = JSON.parse(data.toString());

            // Validate message structure
            if (!this.validateMessage(message)) {
                this.sendError(connection, 'INVALID_MESSAGE', 'Message validation failed');
                return;
            }

            // Update connection info
            connection.updateLastMessage();
            if (message.nodeId && !connection.nodeId) {
                connection.setNodeId(message.nodeId);
                this.workers.set(message.nodeId, connection);
            }

            // Handle message based on type
            const handler = this.messageHandlers.get(message.type);
            if (handler) {
                await handler(connection, message);
            } else {
                console.warn(`Unknown message type: ${message.type}`);
                this.sendError(connection, 'UNKNOWN_MESSAGE_TYPE', `Unknown message type: ${message.type}`);
            }

            // Update metrics
            const processingTime = Date.now() - startTime;
            this.metrics.recordMessage(message.type, processingTime, true);

        } catch (error) {
            console.error('Error handling message:', error);
            this.sendError(connection, 'MESSAGE_PROCESSING_ERROR', error.message);

            const processingTime = Date.now() - startTime;
            this.metrics.recordMessage('UNKNOWN', processingTime, false);
        }
    }

    /**
     * Handle worker state update
     * @param {WorkerConnection} connection
     * @param {Object} message
     */
    async handleStateUpdate(connection, message) {
        const { nodeId, pods, resources, timestamp } = message;

        console.log(`State update from ${nodeId}: ${pods.length} pods`);

        // Validate timestamp (reject old updates)
        const messageAge = Date.now() - timestamp;
        if (messageAge > 10000) { // 10 seconds
            console.warn(`Old state update from ${nodeId}: ${messageAge}ms old`);
            return;
        }

        // Update worker state
        connection.updateState({
            pods,
            resources,
            lastUpdate: timestamp
        });

        // Emit event for cache and other components
        this.emit('workerStateUpdate', {
            nodeId,
            pods,
            resources,
            timestamp,
            connection
        });

        // Send acknowledgment
        this.sendAck(connection, message.messageId);
    }

    /**
     * Handle heartbeat from worker
     * @param {WorkerConnection} connection
     * @param {Object} message
     */
    async handleHeartbeat(connection, message) {
        connection.updateHeartbeat();

        // Send heartbeat response
        this.sendMessage(connection, {
            type: 'HEARTBEAT_ACK',
            timestamp: Date.now(),
            serverTime: Date.now()
        });
    }

    /**
     * Handle pod status update
     * @param {WorkerConnection} connection
     * @param {Object} message
     */
    async handlePodStatus(connection, message) {
        const { podId, status, timestamp } = message;

        this.emit('podStatusUpdate', {
            nodeId: connection.nodeId,
            podId,
            status,
            timestamp
        });

        this.sendAck(connection, message.messageId);
    }

    /**
     * Handle node status update
     * @param {WorkerConnection} connection
     * @param {Object} message
     */
    async handleNodeStatus(connection, message) {
        const { status, conditions, timestamp } = message;

        connection.updateNodeStatus(status, conditions);

        this.emit('nodeStatusUpdate', {
            nodeId: connection.nodeId,
            status,
            conditions,
            timestamp
        });

        this.sendAck(connection, message.messageId);
    }

    /**
     * Handle error report from worker
     * @param {WorkerConnection} connection
     * @param {Object} message
     */
    async handleErrorReport(connection, message) {
        const { error, context, timestamp } = message;

        console.error(`Error from worker ${connection.nodeId}:`, error);

        this.emit('workerError', {
            nodeId: connection.nodeId,
            error,
            context,
            timestamp
        });
    }

    /**
     * Handle resource metrics from worker
     * @param {WorkerConnection} connection
     * @param {Object} message
     */
    async handleResourceMetrics(connection, message) {
        const { metrics, timestamp } = message;

        this.emit('resourceMetrics', {
            nodeId: connection.nodeId,
            metrics,
            timestamp
        });
    }

    /**
     * Send command to specific worker
     * @param {string} nodeId
     * @param {Object} command
     * @returns {Promise}
     */
    async sendCommand(nodeId, command) {
        const connection = this.workers.get(nodeId);
        if (!connection || !connection.isConnected()) {
            throw new Error(`Worker ${nodeId} not connected`);
        }

        return this.sendMessage(connection, {
            type: command.type,
            ...command,
            messageId: this.generateMessageId(),
            timestamp: Date.now()
        });
    }

    /**
     * Send CREATE_POD command to worker
     * @param {string} nodeId
     * @param {Object} podSpec
     * @param {string} walrusBlobId
     * @returns {Promise}
     */
    async createPod(nodeId, podSpec, walrusBlobId) {
        return this.sendCommand(nodeId, {
            type: this.commandTypes.CREATE_POD,
            podSpec,
            walrusBlobId,
            priority: 'HIGH'
        });
    }

    /**
     * Send DELETE_POD command to worker
     * @param {string} nodeId
     * @param {string} podId
     * @returns {Promise}
     */
    async deletePod(nodeId, podId) {
        return this.sendCommand(nodeId, {
            type: this.commandTypes.DELETE_POD,
            podId,
            priority: 'HIGH'
        });
    }

    /**
     * Send UPDATE_POD command to worker
     * @param {string} nodeId
     * @param {string} podId
     * @param {Object} updates
     * @returns {Promise}
     */
    async updatePod(nodeId, podId, updates) {
        return this.sendCommand(nodeId, {
            type: this.commandTypes.UPDATE_POD,
            podId,
            updates,
            priority: 'MEDIUM'
        });
    }

    /**
     * Execute command in pod
     * @param {string} nodeId
     * @param {string} podId
     * @param {string} container
     * @param {Array} command
     * @returns {Promise}
     */
    async executeCommand(nodeId, podId, container, command) {
        return this.sendCommand(nodeId, {
            type: this.commandTypes.EXECUTE_COMMAND,
            podId,
            container,
            command,
            priority: 'MEDIUM'
        });
    }

    /**
     * Broadcast command to all workers
     * @param {Object} command
     * @returns {Promise}
     */
    async broadcastCommand(command) {
        const promises = [];

        for (const [nodeId, connection] of this.workers) {
            if (connection.isConnected()) {
                promises.push(this.sendCommand(nodeId, command));
            }
        }

        return Promise.allSettled(promises);
    }

    /**
     * Get connected workers
     * @returns {Array}
     */
    getConnectedWorkers() {
        const workers = [];

        for (const [nodeId, connection] of this.workers) {
            if (connection.isConnected()) {
                workers.push({
                    nodeId,
                    lastHeartbeat: connection.lastHeartbeat,
                    lastMessage: connection.lastMessage,
                    state: connection.state
                });
            }
        }

        return workers;
    }

    /**
     * Get communication metrics
     * @returns {Object}
     */
    getMetrics() {
        return {
            ...this.metrics.getMetrics(),
            connectedWorkers: this.workers.size,
            connectionPool: this.connectionPool.getStats()
        };
    }

    // Private methods

    sendMessage(connection, message) {
        return connection.send(message);
    }

    sendAck(connection, messageId) {
        if (!messageId) return;

        this.sendMessage(connection, {
            type: 'ACK',
            messageId,
            timestamp: Date.now()
        });
    }

    sendError(connection, errorCode, errorMessage) {
        this.sendMessage(connection, {
            type: 'ERROR',
            errorCode,
            errorMessage,
            timestamp: Date.now()
        });
    }

    validateMessage(message) {
        return message &&
               typeof message.type === 'string' &&
               typeof message.timestamp === 'number';
    }

    generateMessageId() {
        return `${Date.now()}-${Math.random().toString(36).substr(2, 9)}`;
    }

    handleDisconnection(connection, code, reason) {
        console.log(`Worker disconnected: ${connection.nodeId || 'unknown'} (${code}: ${reason})`);

        if (connection.nodeId) {
            this.workers.delete(connection.nodeId);
        }

        this.connectionPool.removeConnection(connection);
        this.metrics.recordDisconnection();

        this.emit('workerDisconnected', {
            nodeId: connection.nodeId,
            code,
            reason
        });
    }

    handleConnectionError(connection, error) {
        console.error(`Connection error for ${connection.nodeId || 'unknown'}:`, error);
        this.metrics.recordError();
    }

    handleServerError(error) {
        console.error('WebSocket server error:', error);
        this.emit('serverError', error);
    }

    startHeartbeatWorker() {
        setInterval(() => {
            const now = Date.now();

            for (const [nodeId, connection] of this.workers) {
                // Check for stale connections
                if (now - connection.lastHeartbeat > this.config.heartbeatInterval * 2) {
                    console.warn(`Stale connection detected: ${nodeId}`);
                    connection.close();
                    continue;
                }

                // Send ping to check connection
                if (connection.isConnected()) {
                    connection.ping();
                }
            }
        }, this.config.heartbeatInterval);
    }

    startMessageProcessor() {
        setInterval(() => {
            this.messageQueue.processMessages();
        }, 100); // Process every 100ms
    }

    startMetricsCollector() {
        setInterval(() => {
            this.metrics.collect();
        }, 60000); // Collect every minute
    }
}

/**
 * Worker Node WebSocket Client
 * Maintains connection to TEE master and handles commands
 */
class WorkerNodeClient extends EventEmitter {
    constructor(nodeId, teeEndpoint, options = {}) {
        super();

        this.nodeId = nodeId;
        this.teeEndpoint = teeEndpoint;

        this.config = {
            reconnectInterval: options.reconnectInterval || 5000,
            maxReconnectAttempts: options.maxReconnectAttempts || -1, // infinite
            heartbeatInterval: options.heartbeatInterval || 30000,
            updateInterval: options.updateInterval || 1000,
            messageTimeout: options.messageTimeout || 30000,
            ...options
        };

        this.ws = null;
        this.reconnectAttempts = 0;
        this.isReconnecting = false;
        this.lastHeartbeat = Date.now();
        this.pendingMessages = new Map(); // messageId -> {resolve, reject, timeout}

        // Local state
        this.state = {
            pods: [],
            resources: {},
            nodeStatus: 'Ready',
            lastUpdate: Date.now()
        };

        // Message handlers
        this.commandHandlers = new Map([
            ['CREATE_POD', this.handleCreatePod.bind(this)],
            ['DELETE_POD', this.handleDeletePod.bind(this)],
            ['UPDATE_POD', this.handleUpdatePod.bind(this)],
            ['EXECUTE_COMMAND', this.handleExecuteCommand.bind(this)],
            ['UPDATE_CONFIG', this.handleUpdateConfig.bind(this)],
            ['COLLECT_LOGS', this.handleCollectLogs.bind(this)],
            ['HEALTH_CHECK', this.handleHealthCheck.bind(this)]
        ]);

        this.connect();
        this.startStateUpdater();
        this.startHeartbeat();
    }

    /**
     * Connect to TEE master
     */
    connect() {
        if (this.ws && this.ws.readyState === WebSocket.OPEN) {
            return;
        }

        console.log(`Connecting to TEE master: ${this.teeEndpoint}`);

        try {
            this.ws = new WebSocket(this.teeEndpoint);

            this.ws.on('open', this.handleOpen.bind(this));
            this.ws.on('message', this.handleMessage.bind(this));
            this.ws.on('close', this.handleClose.bind(this));
            this.ws.on('error', this.handleError.bind(this));
            this.ws.on('ping', this.handlePing.bind(this));

        } catch (error) {
            console.error('Failed to create WebSocket connection:', error);
            this.scheduleReconnect();
        }
    }

    /**
     * Handle connection open
     */
    handleOpen() {
        console.log('Connected to TEE master');
        this.reconnectAttempts = 0;
        this.isReconnecting = false;
        this.lastHeartbeat = Date.now();

        // Send initial registration
        this.sendMessage({
            type: 'WORKER_REGISTER',
            nodeId: this.nodeId,
            capabilities: this.getNodeCapabilities(),
            timestamp: Date.now()
        });

        this.emit('connected');
    }

    /**
     * Handle incoming message
     * @param {Buffer} data
     */
    async handleMessage(data) {
        try {
            const message = JSON.parse(data.toString());

            // Handle system messages
            switch (message.type) {
                case 'ACK':
                    this.handleAck(message);
                    return;

                case 'ERROR':
                    this.handleErrorMessage(message);
                    return;

                case 'HEARTBEAT_ACK':
                    this.lastHeartbeat = Date.now();
                    return;
            }

            // Handle commands
            const handler = this.commandHandlers.get(message.type);
            if (handler) {
                const result = await handler(message);

                // Send response if message has ID
                if (message.messageId) {
                    this.sendMessage({
                        type: 'COMMAND_RESPONSE',
                        messageId: message.messageId,
                        result,
                        success: true,
                        timestamp: Date.now()
                    });
                }
            } else {
                console.warn(`Unknown command type: ${message.type}`);
            }

        } catch (error) {
            console.error('Error handling message:', error);

            // Send error response if possible
            if (message && message.messageId) {
                this.sendMessage({
                    type: 'COMMAND_RESPONSE',
                    messageId: message.messageId,
                    error: error.message,
                    success: false,
                    timestamp: Date.now()
                });
            }
        }
    }

    /**
     * Handle connection close
     * @param {number} code
     * @param {string} reason
     */
    handleClose(code, reason) {
        console.log(`Connection closed: ${code} - ${reason}`);
        this.emit('disconnected', { code, reason });

        if (!this.isReconnecting && this.config.maxReconnectAttempts !== 0) {
            this.scheduleReconnect();
        }
    }

    /**
     * Handle connection error
     * @param {Error} error
     */
    handleError(error) {
        console.error('WebSocket error:', error);
        this.emit('error', error);
    }

    /**
     * Handle ping from server
     */
    handlePing() {
        this.ws.pong();
    }

    /**
     * Send message to TEE master
     * @param {Object} message
     * @returns {Promise}
     */
    sendMessage(message) {
        return new Promise((resolve, reject) => {
            if (!this.ws || this.ws.readyState !== WebSocket.OPEN) {
                reject(new Error('WebSocket not connected'));
                return;
            }

            try {
                const messageStr = JSON.stringify(message);
                this.ws.send(messageStr);

                // Handle messages with IDs (for responses)
                if (message.messageId) {
                    const timeout = setTimeout(() => {
                        this.pendingMessages.delete(message.messageId);
                        reject(new Error('Message timeout'));
                    }, this.config.messageTimeout);

                    this.pendingMessages.set(message.messageId, {
                        resolve,
                        reject,
                        timeout
                    });
                } else {
                    resolve();
                }

            } catch (error) {
                reject(error);
            }
        });
    }

    /**
     * Update local state and send to TEE master
     * @param {Object} updates
     */
    updateState(updates) {
        Object.assign(this.state, updates);
        this.state.lastUpdate = Date.now();

        // Send state update to TEE master
        this.sendStateUpdate();
    }

    /**
     * Send state update to TEE master
     */
    sendStateUpdate() {
        if (!this.ws || this.ws.readyState !== WebSocket.OPEN) {
            return;
        }

        this.sendMessage({
            type: 'STATE_UPDATE',
            nodeId: this.nodeId,
            pods: this.state.pods,
            resources: this.state.resources,
            nodeStatus: this.state.nodeStatus,
            timestamp: Date.now(),
            messageId: this.generateMessageId()
        }).catch(error => {
            console.error('Failed to send state update:', error);
        });
    }

    /**
     * Send heartbeat to TEE master
     */
    sendHeartbeat() {
        if (!this.ws || this.ws.readyState !== WebSocket.OPEN) {
            return;
        }

        this.sendMessage({
            type: 'HEARTBEAT',
            nodeId: this.nodeId,
            timestamp: Date.now()
        }).catch(error => {
            console.error('Failed to send heartbeat:', error);
        });
    }

    // Command handlers

    async handleCreatePod(message) {
        const { podSpec, walrusBlobId } = message;

        console.log(`Creating pod: ${podSpec.metadata.name}`);

        try {
            // Download container image from Walrus if needed
            if (walrusBlobId) {
                await this.downloadFromWalrus(walrusBlobId);
            }

            // Create pod using container runtime
            const podId = await this.createPodInRuntime(podSpec);

            // Update local state
            this.state.pods.push({
                id: podId,
                spec: podSpec,
                status: 'Pending',
                createdAt: Date.now()
            });

            return { podId, status: 'Created' };

        } catch (error) {
            console.error('Failed to create pod:', error);
            throw error;
        }
    }

    async handleDeletePod(message) {
        const { podId } = message;

        console.log(`Deleting pod: ${podId}`);

        try {
            // Delete pod from container runtime
            await this.deletePodFromRuntime(podId);

            // Update local state
            this.state.pods = this.state.pods.filter(pod => pod.id !== podId);

            return { podId, status: 'Deleted' };

        } catch (error) {
            console.error('Failed to delete pod:', error);
            throw error;
        }
    }

    async handleUpdatePod(message) {
        const { podId, updates } = message;

        console.log(`Updating pod: ${podId}`);

        try {
            // Apply updates to pod
            await this.updatePodInRuntime(podId, updates);

            // Update local state
            const pod = this.state.pods.find(p => p.id === podId);
            if (pod) {
                Object.assign(pod, updates);
            }

            return { podId, status: 'Updated' };

        } catch (error) {
            console.error('Failed to update pod:', error);
            throw error;
        }
    }

    async handleExecuteCommand(message) {
        const { podId, container, command } = message;

        console.log(`Executing command in pod ${podId}: ${command.join(' ')}`);

        try {
            const result = await this.executeInContainer(podId, container, command);
            return { output: result };

        } catch (error) {
            console.error('Failed to execute command:', error);
            throw error;
        }
    }

    async handleUpdateConfig(message) {
        const { config } = message;

        console.log('Updating worker configuration');

        try {
            // Update worker configuration
            Object.assign(this.config, config);
            return { status: 'ConfigUpdated' };

        } catch (error) {
            console.error('Failed to update config:', error);
            throw error;
        }
    }

    async handleCollectLogs(message) {
        const { podId, container, lines } = message;

        try {
            const logs = await this.getContainerLogs(podId, container, lines);
            return { logs };

        } catch (error) {
            console.error('Failed to collect logs:', error);
            throw error;
        }
    }

    async handleHealthCheck(message) {
        try {
            const health = await this.performHealthCheck();
            return { health };

        } catch (error) {
            console.error('Health check failed:', error);
            throw error;
        }
    }

    // Helper methods

    handleAck(message) {
        const pending = this.pendingMessages.get(message.messageId);
        if (pending) {
            clearTimeout(pending.timeout);
            this.pendingMessages.delete(message.messageId);
            pending.resolve();
        }
    }

    handleErrorMessage(message) {
        console.error('Error from TEE master:', message.errorMessage);

        const pending = this.pendingMessages.get(message.messageId);
        if (pending) {
            clearTimeout(pending.timeout);
            this.pendingMessages.delete(message.messageId);
            pending.reject(new Error(message.errorMessage));
        }
    }

    scheduleReconnect() {
        if (this.isReconnecting) return;

        if (this.config.maxReconnectAttempts > 0 &&
            this.reconnectAttempts >= this.config.maxReconnectAttempts) {
            console.error('Max reconnect attempts reached');
            this.emit('maxReconnectAttemptsReached');
            return;
        }

        this.isReconnecting = true;
        this.reconnectAttempts++;

        const delay = Math.min(this.config.reconnectInterval * this.reconnectAttempts, 30000);
        console.log(`Scheduling reconnect attempt ${this.reconnectAttempts} in ${delay}ms`);

        setTimeout(() => {
            this.isReconnecting = false;
            this.connect();
        }, delay);
    }

    startStateUpdater() {
        setInterval(() => {
            if (this.ws && this.ws.readyState === WebSocket.OPEN) {
                this.sendStateUpdate();
            }
        }, this.config.updateInterval);
    }

    startHeartbeat() {
        setInterval(() => {
            if (this.ws && this.ws.readyState === WebSocket.OPEN) {
                this.sendHeartbeat();
            }
        }, this.config.heartbeatInterval);
    }

    generateMessageId() {
        return `${Date.now()}-${Math.random().toString(36).substr(2, 9)}`;
    }

    getNodeCapabilities() {
        return {
            cpu: '4',
            memory: '8Gi',
            storage: '100Gi',
            containerRuntime: 'containerd',
            operatingSystem: 'linux',
            architecture: 'amd64'
        };
    }

    // Placeholder methods for container runtime integration
    async downloadFromWalrus(blobId) {
        // Implement Walrus blob download
        console.log(`Downloading blob from Walrus: ${blobId}`);
    }

    async createPodInRuntime(podSpec) {
        // Implement pod creation in container runtime
        return `pod-${Date.now()}`;
    }

    async deletePodFromRuntime(podId) {
        // Implement pod deletion
        console.log(`Deleting pod from runtime: ${podId}`);
    }

    async updatePodInRuntime(podId, updates) {
        // Implement pod updates
        console.log(`Updating pod in runtime: ${podId}`);
    }

    async executeInContainer(podId, container, command) {
        // Implement command execution in container
        return `Command output for: ${command.join(' ')}`;
    }

    async getContainerLogs(podId, container, lines) {
        // Implement log collection
        return [`Log line 1`, `Log line 2`];
    }

    async performHealthCheck() {
        // Implement health check
        return {
            status: 'Healthy',
            checks: {
                containerRuntime: 'OK',
                diskSpace: 'OK',
                network: 'OK'
            }
        };
    }

    disconnect() {
        if (this.ws) {
            this.config.maxReconnectAttempts = 0; // Prevent reconnection
            this.ws.close();
        }
    }
}

/**
 * Worker connection wrapper
 */
class WorkerConnection {
    constructor(ws, config) {
        this.ws = ws;
        this.config = config;
        this.nodeId = null;
        this.lastMessage = Date.now();
        this.lastHeartbeat = Date.now();
        this.lastPong = Date.now();
        this.state = {
            pods: [],
            resources: {},
            nodeStatus: 'Unknown'
        };
    }

    setNodeId(nodeId) {
        this.nodeId = nodeId;
    }

    updateLastMessage() {
        this.lastMessage = Date.now();
    }

    updateHeartbeat() {
        this.lastHeartbeat = Date.now();
    }

    updateLastPong() {
        this.lastPong = Date.now();
    }

    updateState(state) {
        Object.assign(this.state, state);
    }

    updateNodeStatus(status, conditions) {
        this.state.nodeStatus = status;
        this.state.conditions = conditions;
    }

    isConnected() {
        return this.ws && this.ws.readyState === WebSocket.OPEN;
    }

    send(message) {
        return new Promise((resolve, reject) => {
            if (!this.isConnected()) {
                reject(new Error('Connection not open'));
                return;
            }

            try {
                this.ws.send(JSON.stringify(message));
                resolve();
            } catch (error) {
                reject(error);
            }
        });
    }

    ping() {
        if (this.isConnected()) {
            this.ws.ping();
        }
    }

    close() {
        if (this.ws) {
            this.ws.close();
        }
    }
}

/**
 * Connection pool manager
 */
class ConnectionPool {
    constructor(maxConnections) {
        this.maxConnections = maxConnections;
        this.connections = new Set();
        this.stats = {
            total: 0,
            active: 0,
            peak: 0
        };
    }

    addConnection(connection) {
        if (this.connections.size >= this.maxConnections) {
            throw new Error('Connection pool full');
        }

        this.connections.add(connection);
        this.stats.total++;
        this.stats.active++;
        this.stats.peak = Math.max(this.stats.peak, this.stats.active);
    }

    removeConnection(connection) {
        this.connections.delete(connection);
        this.stats.active--;
    }

    getStats() {
        return { ...this.stats };
    }
}

/**
 * Priority message queue
 */
class PriorityMessageQueue {
    constructor() {
        this.queues = {
            HIGH: [],
            MEDIUM: [],
            LOW: []
        };
    }

    addMessage(message, priority = 'MEDIUM') {
        this.queues[priority].push(message);
    }

    processMessages() {
        // Process high priority first
        for (const priority of ['HIGH', 'MEDIUM', 'LOW']) {
            const queue = this.queues[priority];
            if (queue.length > 0) {
                const message = queue.shift();
                this.processMessage(message);
                break; // Process one message per cycle
            }
        }
    }

    processMessage(message) {
        // Process message
        console.log('Processing message:', message.type);
    }
}

/**
 * Communication metrics collector
 */
class CommunicationMetrics {
    constructor() {
        this.metrics = {
            totalConnections: 0,
            activeConnections: 0,
            totalMessages: 0,
            messagesByType: new Map(),
            averageProcessingTime: 0,
            errorCount: 0,
            disconnectionCount: 0
        };
    }

    recordConnection() {
        this.metrics.totalConnections++;
        this.metrics.activeConnections++;
    }

    recordDisconnection() {
        this.metrics.activeConnections--;
        this.metrics.disconnectionCount++;
    }

    recordMessage(type, processingTime, success) {
        this.metrics.totalMessages++;

        if (!this.metrics.messagesByType.has(type)) {
            this.metrics.messagesByType.set(type, 0);
        }
        this.metrics.messagesByType.set(type, this.metrics.messagesByType.get(type) + 1);

        // Update average processing time
        this.metrics.averageProcessingTime =
            (this.metrics.averageProcessingTime * (this.metrics.totalMessages - 1) + processingTime) /
            this.metrics.totalMessages;

        if (!success) {
            this.metrics.errorCount++;
        }
    }

    recordError() {
        this.metrics.errorCount++;
    }

    getMetrics() {
        return {
            ...this.metrics,
            messagesByType: Object.fromEntries(this.metrics.messagesByType)
        };
    }

    collect() {
        // Periodic metrics collection
        console.log('Communication Metrics:', this.getMetrics());
    }
}

module.exports = {
    TEEMasterServer,
    WorkerNodeClient,
    WorkerConnection,
    ConnectionPool,
    PriorityMessageQueue,
    CommunicationMetrics
};