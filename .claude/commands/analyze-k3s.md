# /project:analyze-k3s

Analyze K3s source code for DaaS modification. Usage: /project:analyze-k3s <component>

Components:
- agent-registration: Analyze complete agent registration flow
- websocket: Analyze WebSocket tunnel implementation  
- auth: Analyze authentication and token management
- config: Analyze configuration system
- build: Analyze build system

Process:
1. Open k3s-original directory
2. Read specified component files
3. Trace function calls and data flow
4. Document findings in analysis/ directory
5. Identify modification points
6. Create sequence diagrams
7. Update tasks with findings

Always reference:
- Exact file paths
- Line numbers
- Function signatures
- Data structures