# /project:trace-k3s-flow

Trace a specific flow in K3s code. Usage: /project:trace-k3s-flow <flow>

Flows:
- agent-start: From binary execution to running agent
- registration: From agent start to successful registration
- websocket-connect: WebSocket connection establishment
- auth-flow: Complete authentication process
- config-load: Configuration loading sequence

Output format:
1. Starting point (file:line)
2. Step-by-step execution flow
3. Key decision points
4. Data transformations
5. External dependencies
6. Potential modification points

Save output to: analysis/flow-<flow-name>.md