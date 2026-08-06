# ARCHON — Literature Review

Papers with a local PDF are linked to the category subfolders in this directory. A star immediately before a title means it is a must-read / most relevant paper.

The consolidated related-work list for ARCHON, sorted into buckets

**ARCHON's position in one line:** existing work either *checks rules* (B), *shows
architecture state / reconstructs it* (A), *improves the PR view* (C), *analyzes
code at a low level* (D), *studies evolution over history* (E), *produces evidence*
(F), *tracks one artifact type* (G), or *feeds graphs to models to write code* (H).
None compute a **typed architectural delta of a single PR across many edge kinds,
tied to evidence**. That is ARCHON.

---

## Bucket A — Architecture conformance & recovery
*Recover or check the state of the architecture; often need a hand-drawn model.*

- * **Software Architecture Reconstruction: A Process-Oriented Taxonomy** — Ducasse & Pollet — *IEEE TSE* 35(4), 2009. — https://doi.org/10.1109/TSE.2009.19
  > The map of the whole SAR field (Goals / Process / Inputs / Techniques / Outputs). Positions ARCHON as *bottom-up, source-driven, quasi-automatic* reconstruction for *conformance + co-evolution* — but computed **per PR**, which classic one-shot re-documentation isn't.
- * **Software Reflexion Models: Bridging the Gap Between Source and High-Level Models** — Murphy, Notkin, Sullivan — *ACM SIGSOFT FSE* 1995. — https://doi.org/10.1145/222124.222136
- **Software Reflexion Models: Bridging the Gap between Design and Implementation** (extended) — Murphy, Notkin, Sullivan — *IEEE TSE* 27(4), 2001. — https://doi.org/10.1109/32.917525
  > The direct ancestor: intended-vs-actual architecture, flag violations. ARCHON removes the hand-drawn model (auto-recovers it), adds the per-PR delta, typed operational edges, and contracts.
- **Static Architecture-Conformance Checking: An Illustrative Overview** — Passos, Terra, Valente, Diniz, Mendonça — *IEEE Software* 27(5), 2010. — https://doi.org/10.1109/MS.2009.117
  > Survey of conformance techniques (reflexion, DSM, dependency rules) — the lineage of ARCHON's structural gate.
- **A dependency constraint language to manage object-oriented software architectures** — Terra & Valente — *Software: Practice and Experience* 39(12), 2009. — https://doi.org/10.1002/spe.931
  > Declarative dependency constraints — precursor to allow-list rules; ARCHON auto-seeds the baseline instead of hand-writing it.

**Foundations (modularity & dependency structure) — the theory ARCHON rests on:**
- * **On the Criteria To Be Used in Decomposing Systems into Modules** — Parnas — *CACM* 15(12), 1972. — https://doi.org/10.1145/361598.361623 . PDF: [arc_conformance_recovery/Parnas1972_CriteriaDecomposingModules_CACM.pdf](./arc_conformance_recovery/Parnas1972_CriteriaDecomposingModules_CACM.pdf)
  > Information hiding: a module hides decisions behind a surface. Underwrites ARCHON's "contract lives at the public surface" rule and the boundary-locality argument.
- **Using Dependency Models to Manage Complex Software Architecture** — Sangal, Jordan, Sinha, Jackson — *OOPSLA* 2005. — https://doi.org/10.1145/1094811.1094824
- **Exploring the Structure of Complex Software Designs: An Empirical Study of Open Source and Proprietary Code** — MacCormack, Rusnak, Baldwin — *Management Science* 52(7), 2006. — https://doi.org/10.1287/mnsc.1060.0552
- **Design Rules, Vol. 1: The Power of Modularity** — Baldwin & Clark — MIT Press, 2000. — ISBN 9780262024662 *(book; no DOI)*
  > Design Structure Matrix (DSM) lineage: ARCHON's graph is essentially a *typed, versioned, per-PR-diffed DSM*, and these give the modularity metrics (propagation cost, modularity) you can borrow for debt measures.
- **The C4 model for visualising software architecture** — Simon Brown. — https://c4model.com/
  > Levels of architecture diagrams (context/container/component/code) → ARCHON's altitude/projection idea.

**Commercial / tool ecosystem (industrial architecture analysis):**
- **Structure101** (now part of Sonar). — https://www.sonarsource.com/products/structure101/ *(MED — folded into Sonar; page may move)*
- **Lattix** — https://www.lattix.com/ *(MED — site live but returned 403 to the crawler)*
- **Sotograph / Sonargraph** (hello2morrow). — https://www.hello2morrow.com/
  > Recover dependency graphs, detect cycles, monitor erosion. Show *state*, not the per-PR delta; no contracts.
- **Erode** (architecture-drift GitHub Action/CLI vs. a LikeC4/Structurizr model). — https://github.com/erode-app/erode
- **Arch-Engine** (PR-vs-base dependency/topology + policy checks). — https://github.com/tharcyn/arch-engine
  > The two nearest competitors: they diff PR-vs-base architecturally, but require an existing model (Erode) or are rule/dependency-based (Arch-Engine). ARCHON auto-recovers the model and adds interfaces/services/APIs/schemas/contracts.

---

## Bucket B — Dependency / layering rule checkers
*You write rules by hand; they enforce them in CI. Closest to ARCHON's structural gate, but manual and imports-only.*

- **ArchUnit** (Java architecture testing). — https://www.archunit.org/ · https://github.com/TNG/ArchUnit
- **import-linter** (Python import contracts). — https://github.com/seddonym/import-linter
- **deptrac** (PHP layer/dependency rules). — https://github.com/deptrac/deptrac
  > "Layer A may not depend on layer B", checked in CI. ARCHON extracts the architecture automatically (no rules to write) and sees far beyond imports.

---

## Bucket C — PR-level change & review tooling
*Improve what a reviewer sees on a PR — the closest framing to ARCHON, but each answers a different question (risk / code-structure / navigation), not architectural consequence.*

- **Enhanced code reviews using pull request based change impact analysis** (CHID) — Göçmen, Cezayir, Tüzün — *Empirical Software Engineering* 2025. — https://doi.org/10.1007/s10664-024-10600-2
  > Builds a call graph, finds impacted methods + co-changing files, posts a **risk report** on the PR. Answers "how risky / what might break," not "what boundary moved."
- **ChangeViz: Enhancing the GitHub Pull Request Interface with Method Call Information** — Gasparini, Fregnan, Braz, Baum, Bacchelli — *IEEE VISSOFT* 2021. — https://doi.org/10.1109/VISSOFT52517.2021.00022
  > Method definitions + call-sites inline in the PR — a navigation aid, not an architectural delta.
- **ChangePrism: Visualizing the Essence of Code Changes** — Chen, Lanza, Hayashi — *arXiv* 2025. — https://arxiv.org/abs/2508.12649
  > Visualizes commits beyond red/green lines (modifications / refactorings / micro-changes). Good motivation for "text diffs are insufficient," but still code-level.
- **RefactoringMiner 2.0** — Tsantalis, Ketkar, Dig — *IEEE TSE* 48(3), 2022. — https://doi.org/10.1109/TSE.2020.3007722 · https://github.com/tsantalis/RefactoringMiner
  > Detects refactorings (incl. moves) from commit history. Not a competitor — a possible **building block** for ARCHON's "refactor vs. real change" (node identity / empty-delta).

---

## Bucket D — Low-level program graphs
*Represent code as a detailed graph for analysis — a microscope (statements/data-flow), where ARCHON is a map (packages/contracts).*

- * **Modeling and Discovering Vulnerabilities with Code Property Graphs** — Yamaguchi, Golde, Arp, Rieck — *IEEE S&P* 2014. — https://doi.org/10.1109/SP.2014.44
  > The canonical typed code graph (AST+CFG+PDG), queried to find security bugs. Lesson: types on edges carry meaning (like ARCHON's edge kinds); altitude is the difference (statements vs. packages).
- **An Efficient and Scalable Platform for Java Source Code Analysis Using Overlaid Graph Representations** (ProgQuery) — Rodríguez-Prieto, Mycroft, Ortin — *IEEE Access* 8, 2020. — https://doi.org/10.1109/ACCESS.2020.2987631 . PDF: [program_graphs/Rodriguez2020_ProgQuery_JavaGraphs_IEEEAccess.pdf](./program_graphs/Rodriguez2020_ProgQuery_JavaGraphs_IEEEAccess.pdf)
  > A queryable multi-graph platform for Java. Same microscope altitude; proves "code as a queryable graph" works — ARCHON does it one altitude up.
- **Fine-grained and accurate source code differencing** (GumTree) — Falleri, Morandat, Blanc, Martinez, Monperrus — *ASE* 2014. — https://doi.org/10.1145/2642937.2642982
  > The tree-diff algorithm behind ARCHON's (deferred) node-identity work — matching functions across renames/moves so a refactor stays "empty."
- *Infrastructure ARCHON is built on (not research):* Go's `go/packages` — https://pkg.go.dev/golang.org/x/tools/go/packages · `go/types` — https://pkg.go.dev/go/types

---

## Bucket E — Architecture evolution research
*How architecture drifts over history/releases — not a single PR.*

- **Graph-based analysis and prediction for software evolution** — Bhattacharya, Iliofotou, Neamtiu, Faloutsos — *ICSE* 2012. — https://doi.org/10.1109/ICSE.2012.6227173 . PDF: [architecture_evolution/Bhattacharya2012_GraphBased_SoftwareEvolution_ICSE.pdf](./architecture_evolution/Bhattacharya2012_GraphBased_SoftwareEvolution_ICSE.pdf)
  > Models the system as graphs and uses graph metrics over history to **predict** bug severity / maintenance effort. Both "graphs + evolution," but it forecasts trends; ARCHON pins down one PR's change.
- **Predicting Change Propagation in Software Systems** — Hassan & Holt — *ICSM* 2004. — https://doi.org/10.1109/ICSM.2004.1357812
  > Historical co-change to predict ripple effects — the "blast radius" question, over history rather than per-PR.
- **Programs, Life Cycles, and Laws of Software Evolution** — Lehman — *Proc. IEEE* 68(9), 1980. — https://doi.org/10.1109/PROC.1980.11805 *(MED — DOI resolves to IEEE Xplore; title not re-read from page)*
- **Metrics and Laws of Software Evolution — The Nineties View** — Lehman, Ramil, Wernick, Perry, Turski — *IEEE METRICS* 1997. — https://doi.org/10.1109/METRIC.1997.637156
  > Classic "software changes continuously, architecture drifts." Motivation; ARCHON is the concrete per-PR delta.

---

## Bucket F — Contracts, verification & property testing (evidence)
*Techniques that produce evidence that behavior holds — ARCHON's future/contract-coverage layer attaches these to boundaries.*

- * **QuickCheck: A Lightweight Tool for Random Testing of Haskell Programs** — Claessen & Hughes — *ICFP* 2000. — https://doi.org/10.1145/351240.351266 . PDF: [contracts_property_testing/Claessen2000_QuickCheck_ICFP.pdf](./contracts_property_testing/Claessen2000_QuickCheck_ICFP.pdf)
  > Property-based testing. Produces evidence but doesn't know which architectural contract it belongs to — the gap ARCHON's contract-coverage bridges.
- **Hedgehog** (modern property-based testing library). — https://github.com/hedgehogqa/haskell-hedgehog
- **Consumer-Driven Contracts: A Service Evolution Pattern** — Ian Robinson (martinfowler.com) — 2006. — https://martinfowler.com/articles/consumerDrivenContracts.html
  > The pattern behind ARCHON's arrow contracts (a consumer relies only on the subset of a provider's behavior it uses).
- **Dafny: An Automatic Program Verifier for Functional Correctness** — Leino — *LPAR* 2010. — https://doi.org/10.1007/978-3-642-17511-4_20 . PDF: [contracts_property_testing/Leino2010_Dafny_LPAR.pdf](./contracts_property_testing/Leino2010_Dafny_LPAR.pdf)
  > Heavyweight verification — one end of the "evidence type" spectrum ARCHON's obligations can require.

---

## Bucket G — Single-artifact evolution (config / API / schema)
*Each tracks one artifact type in isolation; ARCHON folds config + services + APIs + schemas into one graph.*

- **Characterizing and Detecting Configuration Compatibility Issues in Android Apps** (ConfDroid) — Huang, Wen, Wei, Liu, Cheung — *ASE* 2021. — https://arxiv.org/abs/2109.00300
  > *(Closest match to the "ConfDroid" reference — it's Android configuration-compatibility analysis. Verify this is the intended paper.)* Tracks configuration through code but stops at config; ARCHON turns a config key into an architecture node.
- **API-compatibility / OpenAPI evolution & schema evolution (protobuf, Avro, DB migrations)** — *a research area, not one canonical paper.* Each compares two versions of one schema. ARCHON puts endpoints and schemas inside the same per-PR architectural delta as everything else. *(No single link — treat as an area; add a specific citation if the paper needs one.)*

---

## Bucket H — LLM / ML + code graphs
*Give a model the code's graph structure so it writes / completes / locates code better — strong evidence that "structure beats text," but none produce a reviewable architectural delta.*

**Learning from / feeding code structure to models:**
- **code2vec: Learning Distributed Representations of Code** — Alon, Zilberstein, Levy, Yahav — *POPL* 2019. — https://arxiv.org/abs/1803.09473
- **code2seq: Generating Sequences from Structured Representations of Code** — Alon, Brody, Levy, Yahav — *ICLR* 2019. — https://arxiv.org/abs/1808.01400
  > AST-path structure beats plain tokens for code understanding — early, foundational evidence.
- * **GraphCodeBERT: Pre-training Code Representations with Data Flow** — Guo et al. — *ICLR* 2021. — https://arxiv.org/abs/2009.08366
  > Data-flow (a semantic graph) improves code models over token-only — the canonical "add the graph, get better understanding" result.
- **Universal Representation for Code** — Liu, Nguyen, Karypis, Sengamedu — *PAKDD* 2021. — https://arxiv.org/abs/2103.03116 (Springer DOI 10.1007/978-3-030-75768-7_28) . PDF: [llm_ml_code_graphs/Liu2021_UniversalRepresentationForCode_PAKDD.pdf](./llm_ml_code_graphs/Liu2021_UniversalRepresentationForCode_PAKDD.pdf)
  > GNN embeddings from a control/data-flow code graph. Learning vectors from code graphs (vs. ARCHON's human-readable, diffable graph).

**Repo-scale graphs for LLMs / agents:**
- * **Code Graph Model (CGM): A Graph-Integrated LLM for Repository-Level Software Engineering Tasks** — Tao et al. — *NeurIPS* 2025. — https://proceedings.neurips.cc/paper_files/paper/2025/file/178ae4ba29022eb7bf509c2e27bc8ab8-Paper-Conference.pdf . PDF: [llm_ml_code_graphs/Tao2025_CGM_CodeGraphModel_NeurIPS.pdf](./llm_ml_code_graphs/Tao2025_CGM_CodeGraphModel_NeurIPS.pdf)
  > Injects the repo dependency graph into an LLM (graph-RAG). Direct evidence graphs help LLMs at repo scale; a template for an LLM reviewer over ARCHON's graph.
- * **RepoGraph: Enhancing AI Software Engineering with Repository-level Code Graph** — Ouyang et al. — *ICLR* 2025. — https://arxiv.org/abs/2410.14684
  > Definition-level repo graph, plug-in to coding agents; boosts SWE-bench. Same belief (repo is a graph); helps agents *edit*, ARCHON *reviews*.
- **RepoHyper: Search-Expand-Refine on Semantic Graphs for Repository-Level Code Completion** — Phan, Phan, Nguyen, Bui — *arXiv* 2024. — https://arxiv.org/abs/2403.06095
  > Graph **expansion** to gather context — the same "walk edges to find the blast radius" ARCHON uses around a delta.
- **GraphCoder: Enhancing Repository-Level Code Completion via Code Context Graph-based Retrieval** — Liu et al. — *arXiv* 2024. — https://arxiv.org/abs/2406.07003
  > Control/data-dependence graph beats text-similarity retrieval — "structure beats text," the same logic as typed edges + witnesses.
- **RepoCoder: Repository-Level Code Completion Through Iterative Retrieval and Generation** — Zhang et al. — *EMNLP* 2023. — https://arxiv.org/abs/2303.12570
  > Right context > raw scale for repo-level tasks.
- **Aider "repo map"** (tree-sitter + graph ranking to pick context) — 2023. — https://aider.chat/2023/10/22/repomap.html
  > Practitioner evidence: a ranked repo graph is how real tools feed an LLM a big codebase.

**Can LLMs reason over graphs? (the caveat that shapes ARCHON's design):**
- * **Can Language Models Solve Graph Problems in Natural Language?** (NLGraph) — Wang, Feng, He, Tan, Han, Tsvetkov — *NeurIPS* 2023 (Spotlight). — https://arxiv.org/abs/2305.10037
- * **Talk like a Graph: Encoding Graphs for Large Language Models** — Fatemi, Halcrow, Perozzi — *ICLR* 2024. — https://arxiv.org/abs/2310.04560
  > LLMs are weak, encoding-sensitive graph reasoners → **compute the delta with a tool, hand the LLM the answer**. This is ARCHON's division of labor.
- **From Local to Global: A Graph RAG Approach to Query-Focused Summarization** (GraphRAG) — Edge et al. (Microsoft) — *arXiv* 2024. — https://arxiv.org/abs/2404.16130
  > Graph-structured retrieval beats flat chunks for global questions — an architecture delta is exactly such a global question.

**Repo-level review / feature agents (recent):**
- **LLM Agents Can See Code Repositories** — Ma, Chen, Yang, Shi, Yan, Gu — *ASE* 2026. — https://arxiv.org/abs/2606.14061 · https://github.com/cslsolow/SeeRepo
  > Visual repo graph as agent context cut input tokens up to ~26% with equal/better accuracy — evidence an architecture view helps AI reviewers.
- **RepoReviewer: A Local-First Multi-Agent Architecture for Repository-Level Code Review** — Zhang — *arXiv* 2026. — https://arxiv.org/abs/2603.16107
  > Multi-agent PR review (context synthesis → file review → prioritization → report). A natural baseline to compare against an ARCHON-delta-augmented reviewer.
- **Architecture-Aware Multi-Design Generation for Repository-Level Feature Addition** (RAIM) — Liu, Chen, Pei, Wang, Wang, Zheng — *arXiv* 2026. — https://arxiv.org/abs/2603.01814
  > Uses a repo code graph to locate cross-file changes and select architecturally-sound patches — architecture awareness for *generation*, where ARCHON is for *review*.

**Coding-agent & program-search proposers (the "proposer" layer ARCHON gates):**
- **Evaluating Large Language Models Trained on Code** (Codex) — Chen et al. (OpenAI) — *arXiv* 2021. — https://arxiv.org/abs/2107.03374 . PDF: [agentic_program_evolution/Chen2021_Codex_EvaluatingLLMsOnCode.pdf](./agentic_program_evolution/Chen2021_Codex_EvaluatingLLMsOnCode.pdf)
- **SWE-bench: Can Language Models Resolve Real-World GitHub Issues?** — Jimenez et al. — *ICLR* 2024. — https://arxiv.org/abs/2310.06770 . PDF: [agentic_program_evolution/Jimenez2024_SWEbench_ICLR.pdf](./agentic_program_evolution/Jimenez2024_SWEbench_ICLR.pdf)
- **SWE-agent: Agent-Computer Interfaces Enable Automated Software Engineering** — Yang et al. — *NeurIPS* 2024. — https://arxiv.org/abs/2405.15793 . PDF: [agentic_program_evolution/Yang2024_SWEagent_NeurIPS.pdf](./agentic_program_evolution/Yang2024_SWEagent_NeurIPS.pdf)
- **Mathematical discoveries from program search with large language models** (FunSearch) — Romera-Paredes et al. (DeepMind) — *Nature* 625, 2023. — https://doi.org/10.1038/s41586-023-06924-6
  > These *generate* changes and rely on the test suite as the safety net. ARCHON is complementary: it supplies the architectural boundary + the checkable "empty boundary delta" condition for when a generated change is internal vs. design-changing.

---

## ⚠ Mentioned earlier but NOT verified — do not cite until confirmed
Searches could not confirm these exist as described. No link is given rather than
a fabricated one; verify manually before use.

- **DERT** (a UML-like PR-change visualizer, said to be on SpringerLink). Not found in dblp/arXiv/Springer under this name.
- **Cybewave** (said to embed Mermaid/PlantUML diagram changes in PRs). The domain does not resolve; the described feature matches **Mermaid Diagram Sync** (https://github.com/marketplace/mermaid-diagram-sync) instead — check whether that's what was meant.
- **RepoWise architecture conformance**. The GitHub org found does AI repo-documentation analysis, not architecture conformance; couldn't confirm the tool as described.

---

## How to read this list (the four-column summary)

| Bucket | What they do | ARCHON's difference |
|---|---|---|
| A. Conformance & recovery | Recover/check architecture state | Auto-recovered baseline + **per-PR delta** + contracts |
| B. Dependency rule checkers | Enforce hand-written import rules | Auto-extracted, many edge kinds, delta |
| C. PR change/review tooling | Risk scores / code-structure views | Architectural before/after, not code-level |
| D. Low-level program graphs | Statement/data-flow microscope | Package/contract map altitude |
| E. Architecture evolution | Predict/observe drift over history | One PR, in CI, now |
| F. Contracts & property testing | Produce behavioral evidence | Evidence bound to the contract it guards |
| G. Single-artifact evolution | Track one of config/API/schema | All folded into one graph |
| H. LLM/ML + code graphs | Graphs help models write code | Graph → a reviewable architectural delta |

New Bucket I — Architecture erosion, smells & technical debt
- ⭐ Symptoms of Architecture Erosion in Code Reviews — Li, Soliman, Liang, Avgeriou — ICSA 2022 — https://arxiv.org/abs/2201.01184 (DOI 10.1109/ICSA53651.2022.00011) — your eval dataset #1 (Zenodo 5929788).
- ⭐ Warnings: Violation Symptoms Indicating Architecture Erosion — Li, Liang, Avgeriou — Information & Software Technology 2023 — https://arxiv.org/abs/2212.12168 (DOI 10.1016/j.infsof.2023.107300) — eval dataset #2.
- ⭐ Are Developers Aware of the Architectural Impact of Their Changes? — Paixão et al. — ASE 2017 — https://doi.org/10.1109/ASE.2017.8115622 — eval dataset #3.
- Controlling Software Architecture Erosion: A Survey — de Silva & Balasubramaniam — JSS 2012 — https://doi.org/10.1016/j.jss.2011.07.036
- Identifying Architectural Bad Smells — Garcia et al. — CSMR 2009 — https://doi.org/10.1109/CSMR.2009.59 (+ Toward a Catalogue… QoSA 2009, https://doi.org/10.1007/978-3-642-02351-4_10)
- Arcan: architectural-smell detection — Fontana et al. — ICSA-W 2017 — https://doi.org/10.1109/ICSAW.2017.16
- Architectural Technical Debt Identification: The Research Landscape — Verdecchia et al. — TechDebt@ICSE 2018 — https://doi.org/10.1145/3194164.3194176

Why it matters: this is the literature on what reviewers flag as architectural problems — exactly what ARCHON claims to surface, and exactly what your OpenStack cases are drawn from. Not citing it is the biggest current gap.

High-value adds

New Bucket J — Automated / LLM code review (your AI-reviewer baselines/competitors)
- CodeReviewer: Automating Code Review Activities by Large-Scale Pre-training — Li et al. — ESEC/FSE 2022 — https://arxiv.org/abs/2203.09095
- Using Pre-Trained Models to Boost Code Review Automation — Tufano et al. — ICSE 2022 — https://arxiv.org/abs/2201.06850
- Towards Automating Code Review Activities — Tufano et al. — ICSE 2021 — https://arxiv.org/abs/2101.02518
- LLM review-comment generation, user study — Olewicki et al. — arXiv 2024 — https://arxiv.org/abs/2411.07091 (verify venue)
- (motivation) Expectations, Outcomes & Challenges of Modern Code Review — Bacchelli & Bird — ICSE 2013 — https://doi.org/10.1109/ICSE.2013.6606617 — grounds "review is the bottleneck."

Into H (code graphs for ML/LLMs) — a foundational one is missing:
- ⭐ Learning to Represent Programs with Graphs — Allamanis, Brockschmidt, Khademi — ICLR 2018 — https://arxiv.org/abs/1711.00740 (the seminal graph-NN-on-code paper; predates GraphCodeBERT).
- A Survey of ML for Big Code and Naturalness — Allamanis et al. — CSUR 2018 — https://doi.org/10.1145/3212695
- CodePlan: Repository-level Coding using LLMs and Planning — Bairi et al. — arXiv 2023 — https://arxiv.org/abs/2309.12499 · RepoFusion — Shrivastava et al. — https://arxiv.org/abs/2306.10998

Into G (config / API / schema) — grounding you actually rely on:
- ⭐ Static Extraction of Program Configuration Options — Rabkin & Katz — ICSE 2011 — https://doi.org/10.1145/1985793.1985812 — this is the grounding for your config edges; should definitely be cited.
- APIDiff: Detecting API Breaking Changes + Why and How Java Developers Break APIs — Brito et al. — SANER 2018 — https://doi.org/10.1109/SANER.2018.8330249 · https://doi.org/10.1109/SANER.2018.8330214 — the API-diff analog to ARCHON's surface/API deltas.
- Software Configuration Engineering in Practice (survey) — Sayagh et al. — TSE 2020 — https://doi.org/10.1109/TSE.2018.2867847

Optional / rounding-out (cite if the paper leans that way)

- SAR clustering algorithms: Bunch (Mancoridis et al., IWPC 1998) · ACDC (Tzerpos & Holt, WCRE 2000) — the classic auto-recovery techniques behind the SAR taxonomy. → Bucket A.
- Microservice architecture recovery — Schneider et al. — EMSE 2025 — https://doi.org/10.1007/s10664-025-10686-2 (verify) — relevant since todo-api is a microservice. → A.
- Change Distilling (Fluri et al., TSE 2007, https://doi.org/10.1109/TSE.2007.70731) + CIA survey (Li, Sun, Leung, Zhang, STVR 2013, DOI 10.1002/stvr.1475) — change-extraction / impact-analysis lineage. → C/D.
- Building Evolutionary Architectures (Ford, Parsons, Kua, O'Reilly 2017) — "architectural fitness functions" (ArchUnit is one). → B/F.

