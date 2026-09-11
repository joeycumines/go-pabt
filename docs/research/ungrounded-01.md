!! **GOAL: Design a COMPLETE and WHOLLY compatible and interoperable API as an optional richer way to model behavior trees, with attached node data, inclusive of (if useful) leveraging Go 1.27's upcoming GENERIC METHOD SUPPORT, cutting-edge-BT-research - both ends of the cutting-edge spectrum, that is the VERY BEST, MOST POWERFUL, and MOST SOPHISTICATED examples, both current and FUTURE, in the areas of _automation and robotics_ AND _video game_, e.g. algorithms, approaches, and technical architectures, and all other facets of relevance** !!

Context: https://pkg.go.dev/github.com/joeycumines/go-behaviortree#section-documentation

Video game architecture framing:

Video games long exhausted the scale where it is viable to tick the full tree naively, and therefore operate on principles vaguely aligned with (N.B. PROBABLY WRONG - this is an example): linearising the tree and processing events as a major performance optimisation

Automation and robotics framing:

The following is based on a potentially incomplete but reasonably lengthy survey of the literature _currently_ available across the (potentially too-narrow) spectrum of behavior tree (BT) algorithms spanning foundational reactivity, rigorous search, probabilistic reasoning, and formal verification. At the execution baseline is the original **PA-BT** (Planning and Acting using Behavior Trees) for reactive, incremental tree expansion. To achieve algorithmic completeness and optimality, this foundation can be upgraded with **BT Expansion** for exhaustive symbolic state-space search and **OBTEA** (Optimal Behavior Tree Expansion Algorithm) for uniform-cost backward planning. For scalable, anytime problem-solving, **UHBTP** (benchmarked within the **BTPG** framework) applies domain-independent heuristics, while **HBTP** uses large language models to guide symbolic planners. When dealing with partial observability, **Belief Behavior Trees (BBT)** and **Active Inference** methodologies model sensing, uncertainty, and probabilistic local control. Finally, formal safety and deployment robustness are addressed by **CABTO** (ensuring grounding completeness), **BehaVerify** and **Fiacre** (for runtime and offline model checking), **LTLf synthesis** (for temporal mission goals), and **PDDL execution graph** transformations (for safe causal parallelization).
References:
* **PA-BT:** [ar5iv.labs.arxiv.org/html/1611.00230](https://ar5iv.labs.arxiv.org/html/1611.00230)
* **BT Expansion:** [ojs.aaai.org/index.php/AAAI/article/view/16755](https://ojs.aaai.org/index.php/AAAI/article/view/16755)
* **OBTEA:** [ijcai.org/proceedings/2024/0755.pdf](https://www.ijcai.org/proceedings/2024/0755.pdf)
* **BTPG & UHBTP:** [ijcai.org/proceedings/2025/0969.pdf](https://www.ijcai.org/proceedings/2025/0969.pdf)
* **Belief Behavior Trees (BBT):** [miccol.github.io/files/iros2020.pdf](https://miccol.github.io/files/iros2020.pdf)
* **CABTO:** [ojs.aaai.org/index.php/AAAI/article/view/40936](https://ojs.aaai.org/index.php/AAAI/article/view/40936)
* **BehaVerify:** [arxiv.org/abs/2208.05360](https://arxiv.org/abs/2208.05360)
* **Fiacre-based BTs:** [arxiv.org/pdf/2502.11904v1](https://arxiv.org/pdf/2502.11904v1)
* **LTLf Synthesis:** [arxiv.org/abs/2307.06399](https://arxiv.org/abs/2307.06399)
* **HBTP:** [arxiv.org/abs/2406.00965](https://arxiv.org/abs/2406.00965)
* **Active Inference & BTs:** [arxiv.org/pdf/2011.09756](https://arxiv.org/pdf/2011.09756)
* **PDDL Execution Graphs:** [arxiv.org/abs/2101.01964](https://arxiv.org/abs/2101.01964)