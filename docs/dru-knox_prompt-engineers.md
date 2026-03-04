**Title:** Stop Prompting, Start Engineering: The “Context as Code” Shift  
**Speaker:** Dru Knox – Head of Product & Design at Tessl (formerly Research Scientist leading language modeling teams at Grammarly)  
**Channel:** AI Native Dev  
**Length:** ~45–60 minutes (typical for this format; exact runtime not listed in metadata)  
**Core thesis:**  
We are no longer “prompt engineers” who throw clever instructions at LLMs. We have become **tech leads** managing teams of AI agents. In that role, the quality of our work is no longer determined by prompt quality — it is determined by **context quality**. Context has become the new source code, and it deserves the same professional software-engineering discipline we apply to traditional codebases.

#### 1. Why “Context as Code” is the right mental model
- Traditional code: version-controlled, statically analyzed, unit-tested, CI/CD’d, observable, reusable, auto-updating.
- Context today: usually a giant blob of Markdown or JSON dumped into the prompt window, manually copy-pasted, quickly stale, impossible to test at scale.
- Consequence: non-deterministic, flaky agent behavior that gets worse as workflows grow complex.

Dru argues we should treat context with the same expectations of **correctness, performance, maintainability, and observability** that we demand from production code.

#### 2. The Context Lifecycle (mapping classic dev practices to agents)

| Traditional Dev Practice | Context Engineering Equivalent | Concrete Techniques Mentioned |
|---------------------------|--------------------------------|-------------------------------|
| **Static Analysis / Linting** | LLM-as-Judge validation | Rule-based checks (“must include X”, “never mention Y”, “format must be Z”), schema enforcement, style guides enforced by a smaller judge model before the agent runs. |
| **Unit & Integration Testing** | Parallel scenario stress-testing | Run the same agent 50–100 times across varied but controlled inputs; measure statistical success rate, latency, cost, hallucination rate. Track regressions when context changes. |
| **CI/CD** | Automated context pipelines | Context registries + auto-refresh jobs (pull latest docs, code, customer data, etc.). Versioned context packages that can be promoted across environments. |
| **Observability & Monitoring** | Agent session analytics | Log every context chunk used per session, surface “missing context” signals when agents fail or users correct them, heatmaps of which context pieces are actually used. |
| **Documentation & Reuse** | Context registries / package managers | Reusable, versioned context modules (e.g., “company-policy-v2.3”, “user-profile-schema”). Think npm/pip but for context. |

#### 3. Handling non-determinism head-on
- There is rarely a single “correct” answer in agentic workflows.
- Solution: move from “pass/fail” to **statistical grading** and **distribution metrics**.
  - Example: instead of asking “did the agent write correct code?”, run it 30 times and measure % that pass integration tests, average quality score from an LLM judge, variance, etc.
- Accept variance as a feature (creativity) but put guardrails so the distribution stays within acceptable bounds.

#### 4. Practical recommendations from the talk
- **Formatting matters** — structured context (JSON, YAML, XML with clear schemas) is dramatically more reliable than prose.
- **Validation-first** — run static checks before expensive agent calls.
- **Evals are your new unit tests** — build a regression suite of real user scenarios.
- **Move from static to dynamic context** — auto-updating registries instead of copy-paste.
- **Review-time vs runtime context** — as models get smarter, we may stop stuffing everything into the prompt and instead give agents tools to retrieve/review context on demand (the “review-time” shift).

#### 5. Tooling illustration
Dru uses **Tessl** (his company’s product) throughout as a live example of a platform built around these principles — context versioning, LLM judges, parallel eval runs, observability dashboards, etc. He shows real before/after metrics where adding proper context engineering improved agent success rates from ~40 % to >90 % in complex workflows.

#### 6. Future outlook & nuances
- As frontier models improve, the optimal amount of proactive context in the prompt may actually decrease.
- Future pattern: agents that can intelligently request or review context at runtime rather than having everything pre-loaded.
- Edge cases discussed:
  - Highly creative vs highly deterministic tasks require different context strategies.
  - Multi-agent systems need shared context registries with access controls.
  - Cost/latency trade-offs: more context = higher cost; smart pruning and summarization become critical.
  - Security & privacy: context often contains sensitive data — needs the same treatment as code secrets.

### Why this talk matters right now (broader implications)
We are in the awkward adolescence of agentic AI. Prompt engineering was the “move fast and break things” phase. Context engineering is the “let’s build reliable, production-grade systems” phase. Companies and developers who adopt this mindset early will see dramatically more predictable, maintainable, and scalable AI applications — while everyone else will keep fighting flaky agents and ballooning token costs.

If you watch the video and paste specific sections or timestamps here, I can help you clean them up, reformat, summarize, or expand on any part with additional examples and code patterns.

Would you like:
- A condensed 1-page executive summary?
- Deep-dive notes on any specific section (e.g., LLM judges, context registries, eval frameworks)?
- Or recommendations for open-source tools that implement these ideas today?

Just let me know how I can make this most useful!
