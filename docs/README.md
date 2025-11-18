# AI Intern Agent Documentation

Comprehensive documentation for the AI-powered autonomous software engineering agent.

## 📚 Documentation Index

### Getting Started
- [Main README](../README.md) - Project overview and quick start
- [Architecture Overview](ARCHITECTURE.md) - System architecture and component relationships
- [Setup Guides](../README.md#setup) - Installation and configuration

### Core Concepts
- [Ticket Processing Flow](TICKET_FLOW.md) - End-to-end ticket lifecycle with diagrams
- [Coordinator Details](COORDINATOR.md) - Main orchestration logic and workflow
- [Smart Indexing](INDEXING.md) - File indexing and smart context selection

### Advanced Features
- [Self-Healing System](SELF_HEALING.md) - AI-driven error fixing and retry logic
- [Metrics & Observability](METRICS_DASHBOARD.md) - Prometheus metrics and HTTP dashboard
- [Context Optimization](CONTEXT_OPTIMIZATION.md) - Smart vs simple context strategies

### Integration Guides
- [Multi-Provider Setup](MULTI_PROVIDER_PLAN.md) - Anthropic, Ollama, and local LLM configuration
- [Ollama Setup](OLLAMA_SETUP.md) - Local LLM installation and model selection
- [Next Features Roadmap](NEXT_FEATURES_ROADMAP.md) - Future enhancements and priorities

### API & Components
- [Agent Interface](../internal/ai/agent/agent.go) - AI provider interface
- [Repository Service](../internal/repository/) - Git and GitHub operations
- [Ticketing Service](../internal/ticketing/) - JIRA integration

## 🎯 Quick Navigation

### For New Developers
1. Start with [Architecture Overview](ARCHITECTURE.md) to understand the system
2. Read [Ticket Processing Flow](TICKET_FLOW.md) to see how everything connects
3. Review [Coordinator Details](COORDINATOR.md) for the main control loop
4. Check [CLAUDE.md](../CLAUDE.md) for development patterns

### For Feature Development
1. Review [Self-Healing System](SELF_HEALING.md) for AI-driven quality gates
2. Check [Smart Indexing](INDEXING.md) for context optimization
3. See [Next Features Roadmap](NEXT_FEATURES_ROADMAP.md) for planned enhancements

### For Operations
1. [Metrics Dashboard](METRICS_DASHBOARD.md) for monitoring
2. [Ollama Setup](OLLAMA_SETUP.md) for local LLM deployment
3. [Multi-Provider Setup](MULTI_PROVIDER_PLAN.md) for cost optimization

## 🔍 Key Diagrams

All documentation includes mermaid diagrams that render on GitHub. Key diagrams:

- **System Architecture** - Component relationships and data flow
- **Ticket Processing** - Full lifecycle from JIRA to PR
- **Coordinator State Machine** - Main control loop states
- **Self-Healing Pipeline** - Error detection and fixing workflow
- **Index Building** - Full vs incremental indexing
- **Context Selection** - Smart context scoring algorithm

## 📖 Documentation Standards

All documentation follows these conventions:
- Mermaid diagrams for visual representation
- Code snippets with file references
- Configuration examples
- Performance metrics where applicable
- Links to implementation files

## 🔧 Troubleshooting

Common issues and solutions:
- See [OLLAMA_SETUP.md](OLLAMA_SETUP.md#troubleshooting) for LLM issues
- Check [CLAUDE.md](../CLAUDE.md) for development patterns
- Review test files for usage examples

## 🤝 Contributing

When adding new features:
1. Update relevant documentation
2. Add mermaid diagrams for new flows
3. Include configuration examples
4. Add performance metrics
5. Update this index

## 📝 Version History

- **v2.0** - Self-healing, incremental indexing, metrics dashboard, interactive CLI
- **v1.5** - Multi-provider support (Anthropic + Ollama)
- **v1.0** - Initial release with basic JIRA → GitHub automation
