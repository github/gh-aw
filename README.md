# GitHub Agentic Workflows

<details>
<summary>Agent quick links</summary>

Hello fellow agent! Welcome to GitHub Agentic Workflows = Actions + Agent + Safety.
Here are some pointers to get you started in using this tool.

- Create a new workflow: https://raw.githubusercontent.com/github/gh-aw/main/create.md
- Install: https://raw.githubusercontent.com/github/gh-aw/main/install.md
- Package a workflow repository: https://raw.githubusercontent.com/github/gh-aw/main/package.md
- Reference: https://raw.githubusercontent.com/github/gh-aw/main/.github/aw/github-agentic-workflows.md

</details>

GitHub Agentic Workflows (`gh-aw`) lets developers define AI-powered repository automation in Markdown with YAML frontmatter and run AI agents securely through GitHub Actions. The `gh-aw` GitHub CLI extension compiles each agentic workflow into a standard GitHub Actions workflow.

Use conventional GitHub Actions for deterministic builds, tests, linting, deployments, and reproducible scripts. Add an agentic workflow when a task needs reasoning or interpretation, such as issue triage, pull-request review, CI failure investigation, documentation maintenance, dependency analysis, or repository reporting. GitHub Agentic Workflows complements existing CI/CD; it does not replace it.

Built-in AI engines include GitHub Copilot, Claude Code, OpenAI Codex, Google Gemini, and Pi. Agent jobs are read-only and sandboxed by default, and configured GitHub writes are normally applied through validated `safe-outputs` jobs with scoped permissions.


## Contents

- [Quick Start](#quick-start)
- [How Agentic Workflows work](#how-github-agentic-workflows-works)
- [Security and permissions](#security-and-permissions)
- [Documentation](#documentation)
- [Contributing](#contributing)
- [Community Contributions](#-community-contributions)
- [Related Projects](#related-projects)
- [Workshop](#workshop)

> [!NOTE]
> A [security vulnerability](https://github.com/github/gh-aw/security/advisories/GHSA-8h78-hpm7-29gg) was discovered in versions `>= 0.83.3, < 0.85.4` and, as a result, those releases were retired as a pre-emptive measure.

## Quick Start

Install the GitHub CLI extension:

```bash
gh extension install github/gh-aw
```

Then follow the [GitHub Agentic Workflows quickstart](https://github.github.com/gh-aw/setup/quick-start/) to select an AI engine, add a sample workflow, and run it through GitHub Actions.

## How Agentic Workflows work

An agentic workflow has two parts: YAML frontmatter configures triggers, permissions, tools, and the AI engine; the Markdown body tells the AI agent what to accomplish. The `gh aw compile` command validates this source and generates the `.lock.yml` workflow that GitHub Actions executes. [Learn how GitHub Agentic Workflows works](https://github.github.com/gh-aw/introduction/how-they-work/).

## Security and permissions

Security, permissions, and controlled writes are core design concerns. The supported agent-job path defaults to read-only GitHub access and sandboxed execution. Safe outputs buffer configured writes, validate them, and apply them in separate jobs with scoped permissions. These controls are configurable, so workflow authors must review permissions, tools, network access, and generated files before deployment. [Learn how GitHub Agentic Workflows handles security and permissions](https://github.github.com/gh-aw/introduction/architecture/).

Using agentic workflows in your repository requires careful attention to security considerations and careful human supervision, and even then things can still go wrong. Use it with caution, and at your own risk.

## Documentation

Use the [GitHub Agentic Workflows documentation](https://github.github.com/gh-aw/) for these paths:

- [Create an agentic workflow](https://github.github.com/gh-aw/setup/creating-workflows/)
- [Choose and authenticate an AI engine](https://github.github.com/gh-aw/reference/engines/)
- [Browse GitHub Agentic Workflows examples by task](https://github.github.com/gh-aw/examples/)
- [Review the security architecture](https://github.github.com/gh-aw/introduction/architecture/)
- [Read the GitHub Agentic Workflows FAQ](https://github.github.com/gh-aw/reference/faq/)

For AI agents and retrieval tools, use the published [agent prompt index](https://github.github.com/gh-aw/llms.txt), [full prompt corpus](https://github.github.com/gh-aw/llms-full.txt), and [AI-readable project summary](https://github.github.com/gh-aw/ai/summary.json).

## Contributing

For development setup and contribution guidelines, see [CONTRIBUTING.md](CONTRIBUTING.md).

### Custom Go linters

To build and test repository custom linters:

- `go test ./pkg/linters/<linter-name>/...`
- `go build ./cmd/linters`
- `make golint-custom`

`make golint-custom` builds `cmd/linters` and runs the custom analyzers against `./cmd/...` and `./pkg/...`.



## 🌍 Community Contributions

<sup>Community members whose issues were resolved — updated automatically.</sup>

[@a-sjogren-accenture (2)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Aa-sjogren-accenture)
[@aaronspindler (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Aaaronspindler)
[@abillingsley (2)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Aabillingsley)
[@adam-cobb (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Aadam-cobb)
[@adamhenson (2)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Aadamhenson)
[@adamtasteslikegood (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Aadamtasteslikegood)
[@adhikjoshi (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Aadhikjoshi)
[@ahmadabdalla (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Aahmadabdalla)
[@ajfeldman6 (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Aajfeldman6)
[@AkshatRaj00 (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3AAkshatRaj00)
[@alanpeabody (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Aalanpeabody)
[@alcastaneda (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Aalcastaneda)
[@AlexanderWert (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3AAlexanderWert)
[@AlexDeMichieli (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3AAlexDeMichieli)
[@alexsiilvaa (2)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Aalexsiilvaa)
[@alondahari (17)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Aalondahari)
[@alvistar (2)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Aalvistar)
[@AmoebaChant (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3AAmoebaChant)
[@anthonymastreanvae (11)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Aanthonymastreanvae)
[@aoxiangtianyu-go (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Aaoxiangtianyu-go)
[@apenab (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Aapenab)
[@arabkin (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Aarabkin)
[@arezero (6)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Aarezero)
[@arthurfvives (8)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Aarthurfvives)
[@Artur- (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3AArtur-)
[@asatara (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Aasatara)
[@askpaisa (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Aaskpaisa)
[@askpt (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Aaskpt)
[@astefan (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Aastefan)
[@awoisoak (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Aawoisoak)
[@b-dantas (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Ab-dantas)
[@b2pacific (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Ab2pacific)
[@babaakihiro (2)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Ababaakihiro)
[@bartul (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Abartul)
[@bbonafed (23)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Abbonafed)
[@beardofedu (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Abeardofedu)
[@benissimo (8)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Abenissimo)
[@benvillalobos (12)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Abenvillalobos)
[@blavity-machine-user (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Ablavity-machine-user)
[@blozano-tt (9)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Ablozano-tt)
[@bmerkle (3)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Abmerkle)
[@boydj (2)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Aboydj)
[@Bra1nFartz (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3ABra1nFartz)
[@BrandonLewis (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3ABrandonLewis)
[@brendanlapuma (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Abrendanlapuma)
[@bryanchen-d (23)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Abryanchen-d)
[@bryanknox (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Abryanknox)
[@bshore-bf (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Abshore-bf)
[@Calidus (8)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3ACalidus)
[@camposbrunocampos (2)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Acamposbrunocampos)
[@carlincherry (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Acarlincherry)
[@carlosflorencio (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Acarlosflorencio)
[@CatsMiaow (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3ACatsMiaow)
[@chepa92 (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Achepa92)
[@chrisfregly (2)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Achrisfregly)
[@chrizbo (7)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Achrizbo)
[@CiscoRob (2)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3ACiscoRob)
[@ckittel (2)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Ackittel)
[@cknight (2)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Acknight)
[@clementbolin (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Aclementbolin)
[@cogni-ai-ee (2)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Acogni-ai-ee)
[@consulthys (3)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Aconsulthys)
[@Corb3nik (3)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3ACorb3nik)
[@corygehr (20)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Acorygehr)
[@corymhall (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Acorymhall)
[@crmitchelmore (3)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Acrmitchelmore)
[@DaanLucas (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3ADaanLucas)
[@dagecko (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Adagecko)
[@Daidanny008 (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3ADaidanny008)
[@Dan-Albrecht (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3ADan-Albrecht)
[@Dan-Co (2)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3ADan-Co)
[@danielmeppiel (8)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Adanielmeppiel)
[@danquirk (2)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Adanquirk)
[@darwin-gonzales (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Adarwin-gonzales)
[@davidahmann (3)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Adavidahmann)
[@davidslater (3)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Adavidslater)
[@dbudym-cs (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Adbudym-cs)
[@DeagleGross (3)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3ADeagleGross)
[@devantler (2)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Adevantler)
[@deyaaeldeen (10)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Adeyaaeldeen)
[@dfrysinger (2)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Adfrysinger)
[@dgolombek (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Adgolombek)
[@dholmes (3)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Adholmes)
[@dhrapson (2)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Adhrapson)
[@DimaBir (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3ADimaBir)
[@dkurepa (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Adkurepa)
[@dneimke (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Adneimke)
[@DogeAmazed (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3ADogeAmazed)
[@Dongbumlee (2)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3ADongbumlee)
[@doughgle (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Adoughgle)
[@drehelis (4)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Adrehelis)
[@DrPye (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3ADrPye)
[@dsfaccini (2)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Adsfaccini)
[@dsibilio (4)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Adsibilio)
[@dsolteszopyn (2)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Adsolteszopyn)
[@dsyme (39)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Adsyme)
[@duncankmckinnon (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Aduncankmckinnon)
[@eaftan (3)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Aeaftan)
[@edburns (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Aedburns)
[@edgeq (2)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Aedgeq)
[@elika56 (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Aelika56)
[@emexelem (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Aemexelem)
[@enbw-mmattes (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Aenbw-mmattes)
[@eran-medan (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Aeran-medan)
[@ericchansen (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Aericchansen)
[@ericstj (2)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Aericstj)
[@Esomoire-consultancy-Company (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3AEsomoire-consultancy-Company)
[@Etienne-M (5)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3AEtienne-M)
[@Evangelink (5)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3AEvangelink)
[@fbecar22 (2)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Afbecar22)
[@fchareyr (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Afchareyr)
[@FDevTakima (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3AFDevTakima)
[@ferryhinardi (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Aferryhinardi)
[@flatiron32 (2)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Aflatiron32)
[@florianbader (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Aflorianbader)
[@fr4nc1sc0-r4m0n (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Afr4nc1sc0-r4m0n)
[@funkymonkeyjam (2)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Afunkymonkeyjam)
[@G1Vh (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3AG1Vh)
[@GandrotulaRajesh (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3AGandrotulaRajesh)
[@github-actions (11)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Agithub-actions)
[@github-antoine-brechon (4)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Agithub-antoine-brechon)
[@GKersten (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3AGKersten)
[@glitch-ux (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Aglitch-ux)
[@grahame-white (9)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Agrahame-white)
[@graphaelli (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Agraphaelli)
[@GregoireW (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3AGregoireW)
[@gregsmi (2)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Agregsmi)
[@h-no (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Ah-no)
[@h3y6e (2)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Ah3y6e)
[@haavamoa (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Ahaavamoa)
[@haolpku (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Ahaolpku)
[@harrisoncramer (2)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Aharrisoncramer)
[@heaversm (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Aheaversm)
[@heiskr (7)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Aheiskr)
[@hermanho (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Ahermanho)
[@holwerda (3)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Aholwerda)
[@hpsin (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Ahpsin)
[@hrishikeshathalye (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Ahrishikeshathalye)
[@ianreay (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Aianreay)
[@IEvangelist (14)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3AIEvangelist)
[@ilja (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Ailja)
[@Infinnerty (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3AInfinnerty)
[@insop (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Ainsop)
[@ivancea (9)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Aivancea)
[@j-srodka (6)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Aj-srodka)
[@jamesadevine (4)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Ajamesadevine)
[@JamesNK (4)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3AJamesNK)
[@JanKrivanek (4)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3AJanKrivanek)
[@jaroslawgajewski (27)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Ajaroslawgajewski)
[@JasonYeMSFT (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3AJasonYeMSFT)
[@Jasper13006 (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3AJasper13006)
[@jbaruch (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Ajbaruch)
[@jcooklin (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Ajcooklin)
[@jeffhandley (12)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Ajeffhandley)
[@jeremiah-snee-openx (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Ajeremiah-snee-openx)
[@jfomhover (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Ajfomhover)
[@jhamon (2)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Ajhamon)
[@jitran (5)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Ajitran)
[@JKamsker (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3AJKamsker)
[@joesturge (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Ajoesturge)
[@johndowns (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Ajohndowns)
[@johnpreed (5)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Ajohnpreed)
[@johnwilliams-12 (11)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Ajohnwilliams-12)
[@jonathanpeppers (5)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Ajonathanpeppers)
[@joperezr (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Ajoperezr)
[@JoshGreenslade (2)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3AJoshGreenslade)
[@joshjohanning (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Ajoshjohanning)
[@jsalmassy (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Ajsalmassy)
[@jsoref (2)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Ajsoref)
[@jsquire (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Ajsquire)
[@jtracey93 (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Ajtracey93)
[@kaovilai (4)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Akaovilai)
[@karl-petter-sj (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Akarl-petter-sj)
[@katriendg (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Akatriendg)
[@kbreit-insight (5)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Akbreit-insight)
[@KGoovaer (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3AKGoovaer)
[@kkruel8100 (2)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Akkruel8100)
[@Knufle (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3AKnufle)
[@Krzysztof-Cieslak (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3AKrzysztof-Cieslak)
[@kthompson (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Akthompson)
[@kubaflo (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Akubaflo)
[@labudis (2)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Alabudis)
[@ladamski (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Aladamski)
[@lecoursen (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Alecoursen)
[@lilseyi (2)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Alilseyi)
[@lindeberg (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Alindeberg)
[@lkraav (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Alkraav)
[@loganrosen (6)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Aloganrosen)
[@look (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Alook)
[@lpcox (8)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Alpcox)
[@ludoviclafole (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Aludoviclafole)
[@lukeed (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Alukeed)
[@lupinthe14th (3)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Alupinthe14th)
[@lw396 (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Alw396)
[@m-titov (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Am-titov)
[@maikelvdh (2)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Amaikelvdh)
[@mark-hingston (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Amark-hingston)
[@mason-tim (8)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Amason-tim)
[@matiloti (4)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Amatiloti)
[@mattcosta7 (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Amattcosta7)
[@MatthewBunker (2)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3AMatthewBunker)
[@MatthewLabasan-NBCU (2)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3AMatthewLabasan-NBCU)
[@MattSkala (5)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3AMattSkala)
[@MauroDruwel (4)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3AMauroDruwel)
[@maxbeizer (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Amaxbeizer)
[@maxknv (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Amaxknv)
[@mcantrell (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Amcantrell)
[@mdashrraf (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Amdashrraf)
[@MH0386 (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3AMH0386)
[@mhavelock (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Amhavelock)
[@michen00 (2)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Amichen00)
[@microsasa (10)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Amicrosasa)
[@misrarim (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Amisrarim)
[@mlinksva (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Amlinksva)
[@mnkiefer (13)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Amnkiefer)
[@molson504x (2)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Amolson504x)
[@Mossaka (3)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3AMossaka)
[@mrfelton (5)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Amrfelton)
[@mrjf (7)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Amrjf)
[@MrSanchez (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3AMrSanchez)
[@mstrathman (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Amstrathman)
[@mur6 (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Amur6)
[@mvdbos (2)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Amvdbos)
[@nestele (2)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Anestele)
[@neta-vega (3)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Aneta-vega)
[@NicoAvanzDev (3)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3ANicoAvanzDev)
[@NicolasRannou (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3ANicolasRannou)
[@nihal467 (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Anihal467)
[@Nikhil-Anand-DSG (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3ANikhil-Anand-DSG)
[@NikolajBjorner (2)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3ANikolajBjorner)
[@norrietaylor (9)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Anorrietaylor)
[@not-mksv (3)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Anot-mksv)
[@octatone (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Aoctatone)
[@oscarvalenzuelab (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Aoscarvalenzuelab)
[@PaulAylward2 (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3APaulAylward2)
[@peter-hendy (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Apeter-hendy)
[@petercort (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Apetercort)
[@pethers (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Apethers)
[@pgaskin (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Apgaskin)
[@pholleran (4)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Apholleran)
[@Phonesis (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3APhonesis)
[@Pierrci (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3APierrci)
[@plengauer (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Aplengauer)
[@pmalarme (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Apmalarme)
[@polmichel (2)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Apolmichel)
[@ppusateri (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Appusateri)
[@praveenkuttappan (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Apraveenkuttappan)
[@prpercival (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Aprpercival)
[@PureWeen (10)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3APureWeen)
[@qwert666 (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Aqwert666)
[@r-garcia-de-oliveira (2)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Ar-garcia-de-oliveira)
[@rabo-unumed (4)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Arabo-unumed)
[@racedale (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Aracedale)
[@radiantspace (3)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Aradiantspace)
[@rafael-unloan (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Arafael-unloan)
[@rbstp (5)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Arbstp)
[@reggie-k (4)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Areggie-k)
[@remypanicker (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Aremypanicker)
[@rhardouin (2)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Arhardouin)
[@ricohomewood (2)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Aricohomewood)
[@rmarinho (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Armarinho)
[@robinmam (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Arobinmam)
[@romainh-betclic (3)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Aromainh-betclic)
[@rspurgeon (4)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Arspurgeon)
[@Rubyj (3)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3ARubyj)
[@ruokun-niu (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Aruokun-niu)
[@ryckmansm (2)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Aryckmansm)
[@salekseev (3)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Asalekseev)
[@salmanmkc (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Asalmanmkc)
[@samuelkahessay (30)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Asamuelkahessay)
[@samus-aran (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Asamus-aran)
[@SanthoshNandha (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3ASanthoshNandha)
[@sbodapati-gfm (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Asbodapati-gfm)
[@seangibeault (2)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Aseangibeault)
[@sebastianbiallas (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Asebastianbiallas)
[@seesharprun (6)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Aseesharprun)
[@sg650 (16)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Asg650)
[@shawnHartsell (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3AshawnHartsell)
[@Shazwazza (2)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3AShazwazza)
[@shiran-gutsy (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Ashiran-gutsy)
[@shubhamtanwar23 (3)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Ashubhamtanwar23)
[@siyo-rms (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Asiyo-rms)
[@srgibbs99 (6)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Asrgibbs99)
[@ssulei7 (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Assulei7)
[@stacktick (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Astacktick)
[@stefankrzyz (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Astefankrzyz)
[@steliosfran (3)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Asteliosfran)
[@stephen2002119 (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Astephen2002119)
[@straub (6)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Astraub)
[@strawgate (48)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Astrawgate)
[@susmahad (3)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Asusmahad)
[@swimmesberger (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Aswimmesberger)
[@syarihu (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Asyarihu)
[@szabta89 (3)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Aszabta89)
[@tadelesh (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Atadelesh)
[@Tarekchehahde (2)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3ATarekchehahde)
[@theletterf (25)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Atheletterf)
[@thi-feonir (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Athi-feonir)
[@timdittler (3)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Atimdittler)
[@tinytelly (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Atinytelly)
[@tobio (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Atobio)
[@tomasmed (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Atomasmed)
[@tore-unumed (16)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Atore-unumed)
[@trask (4)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Atrask)
[@tsm-harmoney (5)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Atsm-harmoney)
[@tspascoal (2)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Atspascoal)
[@tvu4-wowcorp (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Atvu4-wowcorp)
[@tylersmalley (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Atylersmalley)
[@UncleBats (2)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3AUncleBats)
[@v1v (4)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Av1v)
[@verkyyi (2)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Averkyyi)
[@veverkap (3)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Aveverkap)
[@ViktorHofer (3)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3AViktorHofer)
[@virenpepper (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Avirenpepper)
[@vishalagrawal-jisr (2)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Avishalagrawal-jisr)
[@whoschek (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Awhoschek)
[@wizardofosmium (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Awizardofosmium)
[@wtgodbe (4)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Awtgodbe)
[@xirzec (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Axirzec)
[@yaananth (1)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Ayaananth)
[@Yoyokrazy (2)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3AYoyokrazy)
[@yskopets (53)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Ayskopets)
[@zarenner (5)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Azarenner)
[@zkoppert (2)](https://github.com/github/gh-aw/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Azkoppert)

GitHub Agentic Workflows is supported by companion projects that provide additional security and integration capabilities:

- **[Agent Workflow Firewall (AWF)](https://github.com/github/gh-aw-firewall)** - Network egress control for AI agents, providing domain-based access controls and activity logging for secure workflow execution
- **[MCP Gateway](https://github.com/github/gh-aw-mcpg)** - Routes Model Context Protocol (MCP) server calls through a unified HTTP gateway for centralized access management
- **[gh-aw-actions](https://github.com/github/gh-aw-actions)** - Shared library of custom GitHub Actions used by compiled workflows, providing functionality such as MCP server file management
## Workshop

> [!TIP]
> **Ready to learn GitHub Agentic Workflows hands-on?** The [**Factory Tour Workshop**](https://github.com/githubnext/gh-aw-workshop) is a self-contained, step-by-step workshop repository designed to teach you how to build, run, and customize agentic workflows from scratch.
