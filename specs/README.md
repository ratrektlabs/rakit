# rl-agent v2 - Specledger.io Setup

## Quick Start

### 1. Install Specledger CLI

```bash
curl -fsSL https://specledger.io/install.sh | bash
```

### 2. Create Account

Go to https://app.specledger.io/login and sign up

### 3. Initialize Project

```bash
cd /path/to/rl-agent
spec init
```

### 4. Link Spec

```bash
spec add ./specs/rl-agent-v2.spec.md
```

### 5. Start Development

```bash
# AI reads spec and generates code
spec implement --component Provider --provider openai

# Or manual implementation with spec validation
spec validate ./provider/openai/openai.go
```

## Workflow

1. **Write/Update Spec** - Edit `specs/rl-agent-v2.spec.md`
2. **Validate** - Run `spec validate` to check implementation matches spec
3. **Implement** - Write code or use AI to generate
4. **Test** - Spec generates tests automatically
5. **Checkpoint** - `spec checkpoint` to save progress

## Spec Structure

The spec defines:
- **6 core components**: Provider, Agent, Tool, Skill, Memory, HTTP Handler
- **Interfaces**: Go interfaces with method signatures
- **Data types**: Request/response structures
- **Contracts**: Behavioral requirements
- **Implementations**: Concrete implementations to build
- **File structure**: Project organization
- **Testing requirements**: What to test
- **Performance/Security**: Non-functional requirements

## Integration with AI

Specledger works with:
- Claude Code
- Gemini CLI
- GitHub Copilot
- Cursor

Example:
```bash
# Claude Code reads spec and implements
claude-code "Implement Provider interface from spec"

# Gemini generates tests from spec
gemini "Generate tests for Agent component based on spec"
```

## Benefits for rl-agent

✅ **Single source of truth** - Spec is the contract
✅ **AI alignment** - All AI tools read same spec
✅ **Validation** - Check code matches spec
✅ **Documentation** - Spec = living docs
✅ **Traceability** - Track changes and decisions
✅ **Onboarding** - New contributors read spec first

## Next Steps

1. Sign up at specledger.io
2. Install CLI
3. Run `spec init` in this directory
4. Import `specs/rl-agent-v2.spec.md`
5. Start implementing from Phase 1

## Resources

- [Specledger Docs](https://specledger.io/docs)
- [Spec-Driven Development Guide](https://specledger.io/blog/)
- [GitHub Spec Kit](https://github.blog/ai-and-ml/generative-ai/spec-driven-development-with-ai-get-started-with-a-new-open-source-toolkit/)
