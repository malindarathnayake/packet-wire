# UDP Switchboard Documentation

This directory contains comprehensive documentation for the UDP Switchboard project.

---

## 📚 Document Index

| Document | Purpose | Audience |
|----------|---------|----------|
| [**udp-switchboard-spec.md**](udp-switchboard-spec.md) | Product specification and requirements | Product, Dev |
| [**implementation-guide.md**](implementation-guide.md) | Code architecture and internals | Developers |
| [**api-reference.md**](api-reference.md) | Public types, functions, methods | Developers |
| [**engineering-standards.md**](engineering-standards.md) | Code standards and patterns | Reviewers, Dev |
| [**feature-dual-mirroring-spec.md**](feature-dual-mirroring-spec.md) | Dual mirroring feature design | Product, Dev |

---

## 🗂️ Document Descriptions

### udp-switchboard-spec.md
**The main specification document.**

Contains:
- System architecture and intent
- Configuration schema (config.yaml, routes.json)
- Startup sequence and behavior
- Error handling matrix
- Metrics and logging specifications
- Deployment notes

### implementation-guide.md
**Deep dive into the codebase structure.**

Contains:
- Architecture diagrams
- Package structure and dependencies
- Core component documentation
- Data flow explanations
- Concurrency model
- Extension points for new features
- Performance characteristics

### api-reference.md
**Complete API documentation.**

Contains:
- All public types and structs
- Function signatures and parameters
- Method documentation
- InfluxDB measurement schemas
- GELF log event schemas

### engineering-standards.md
**Code quality and patterns reference.**

Contains:
- Baseline requirements checklist
- Error propagation patterns
- Concurrency patterns (producer/consumer, reader/writer, shutdown)
- Anti-patterns to avoid
- Retry/backoff standards
- Logging standards
- Code review checklist

### feature-dual-mirroring-spec.md
**Upcoming feature: Dual Mirroring.**

Contains:
- Feature intent and use cases
- Architecture changes
- Configuration schema additions
- Implementation plan
- Testing strategy
- Performance considerations

---

## 🔍 Quick Reference

### Finding Information

| If you need to... | See... |
|-------------------|--------|
| Understand the system architecture | [implementation-guide.md#architecture-overview](implementation-guide.md#architecture-overview) |
| Configure a new route | [udp-switchboard-spec.md#config-schema](udp-switchboard-spec.md#config-schema) |
| Add a new metric | [api-reference.md#metricswriter](api-reference.md#package-metrics) |
| Add a new log event | [api-reference.md#logger](api-reference.md#package-logging) |
| Understand error handling | [implementation-guide.md#error-handling](implementation-guide.md#error-handling) |
| Review code for concurrency | [engineering-standards.md#concurrency-patterns](engineering-standards.md#concurrency-patterns) |
| Add a new feature | [implementation-guide.md#extension-points](implementation-guide.md#extension-points) |

### Common Tasks

#### Adding a New Configuration Option

1. Add struct field in `internal/config/config.go`
2. Add validation in `validateConfig()`
3. Add env override in `applyEnvironmentOverrides()` if needed
4. Update `config/config.yaml` template
5. Document in [udp-switchboard-spec.md](udp-switchboard-spec.md)

#### Adding a New Metric

1. Add method to `MetricsWriter` in `internal/metrics/influx.go`
2. Define measurement name, tags, and fields
3. Call from appropriate location
4. Document in [api-reference.md](api-reference.md#influxdb-measurements-reference)

#### Adding a New Log Event

1. Add method to `Logger` in `internal/logging/gelf.go`
2. Define event name, level, and structured fields
3. Call from appropriate location
4. Document in [api-reference.md](api-reference.md#gelf-log-events-reference)

---

## 📁 Documentation Standards

### Naming Conventions

| Type | Pattern | Example |
|------|---------|---------|
| Spec documents | `{component}-spec.md` | `udp-switchboard-spec.md` |
| Feature specs | `feature-{name}-spec.md` | `feature-dual-mirroring-spec.md` |
| Guides | `{topic}-guide.md` | `implementation-guide.md` |
| References | `{topic}-reference.md` | `api-reference.md` |

### Document Structure

All documents should include:
- Title and brief description
- Table of contents (for long documents)
- Clear section headings
- Code examples where applicable
- Tables for structured data

---

## 🔄 Keeping Documentation Updated

When making code changes:

1. **New features** → Update implementation-guide.md, api-reference.md
2. **Config changes** → Update udp-switchboard-spec.md
3. **New patterns** → Update engineering-standards.md
4. **Breaking changes** → Update all affected documents

### Document Review Checklist

- [ ] Code examples compile/run
- [ ] Links are valid
- [ ] Tables are properly formatted
- [ ] Diagrams reflect current architecture
- [ ] Version-specific info is noted
