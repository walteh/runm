# Docker Compose Test Scenario

This test scenario validates Docker Compose functionality with runm, simulating a multi-container application with:

- **web**: nginx web server serving static content
- **api**: node.js API service  
- **db**: PostgreSQL database

## Architecture

```
┌─────────────────┐    ┌─────────────────┐    ┌─────────────────┐
│      web        │───▶│      api        │───▶│       db        │
│   (nginx:alpine)│    │ (node:18-alpine)│    │(postgres:15-alpine)│
│   port: 8080    │    │   port: 3000    │    │   port: 5432    │
└─────────────────┘    └─────────────────┘    └─────────────────┘
```

## Expected Behavior

1. All three services should start successfully
2. Service dependencies should be respected (db → api → web)
3. Named volumes should be created for postgres data
4. Port forwarding should work for all exposed ports
5. Inter-service networking should allow communication

## Container Groups / Kata Integration

This scenario is designed to test how runm handles:
- Multi-container orchestration 
- Container groups (if implemented via kata-style container groups)
- Service discovery and networking between containers
- Volume management across containers
- Dependency ordering