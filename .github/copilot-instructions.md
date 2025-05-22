# Overview

This repo contains networking components and controllers for the Fleet project. It is primarily written in Go and leverages Kubernetes client-go and controller-runtime libraries. The repository is organized as a monorepo, with packages grouped by functionality.

## General Rules

- Use @terminal when answering questions about Git.
- If you're waiting for my confirmation ("OK"), proceed without further prompting.
- Follow the [Go Style Guide](https://go.dev/wiki/Style) and [Uber Go Style Guide](https://github.com/uber-go/guide/blob/master/style.md) if possible. If there are any conflicts, follow the Go Style Guide.
- Favor using the standard library over third-party libraries.
- Run goimports on save.
- Run golint and go vet to check for errors.
- Use go mod tidy if the dependencies are changed.

## Terminology

- **Fleet**: A collection of Kubernetes clusters managed as a unit.
- **Networking Controller**: A controller responsible for managing network resources across clusters.
- **Hub cluster**: An Kubernetes cluster that hosts the control plane of the fleet.
- **Member cluster**: A Kubernetes cluster that is part of the fleet.
- **Fleet-system Namespace**: A reserved namespace in all clusters for running Fleet networking controllers and putting internal resources.
- **Reserved member cluster namespace in the hub cluster**: A reserved namespace in the hub cluster where a member cluster can access to communicate with the hub cluster.

## Repository directory structure

- `api/` - Contains Golang structs for CRDs.
- `charts/` - Helm charts for deploying networking components.
  - `charts/hub-net-controller-manager/` - Helm chart for the fleet hub networking components.
    - `charts/mcs-controller-manager/` - Helm chart for the fleet member networking component which provides the L4 load balancing functionality.
    - `charts/member-net-controller-manager/` - Helm chart for the fleet member networking components.
- `cmd/` - Entry points for networking controllers and agents.
  - `cmd/hub-net-controller-manager/` - Main entry point for the hub networking components.
  - `cmd/mcs-controller-manager/` - Main entry point for the member networking component which provides the L4 load balancing functionality.
  - `cmd/member-net-controller-manager/` - Main entry point for the member networking components.
- `config/` - CRD manifests and configuration files built from the CRD definitions in the `api/` folder.
  - `config/crd/bases` - CRD definitions for the networking features.
- `docker/` - Dockerfiles for building images for networking components.
- `examples/` - Example YAMLs for CRDs and networking resources.
- `hack/` - Scripts and tools for development.
- `pkg/` - Libraries and core logic for networking controllers.
  - `pkg/common/` - Common libraries shared between networking controllers.
  - `pkg/controllers/` - Core networking controllers.
     - `pkg/controllers/hub/` - Hub networking controller logic.
     - `pkg/controllers/member/` - Member networking controller logic.
     - `pkg/controllers/mcs/` - MCS networking controller logic.
- `test/` - Integration, and e2e tests.
    - `test/apis/` - The tests for the CRD definitions.
    - `test/common/` - Common test utilities and helpers.
    - `test/e2e/` - End-to-end tests for networking components.
    - `test/perftest/` - Performance tests for networking components.
    - `test/scripts/` - Scripts to setup and clean the e2e test enviroments.
- `tools/` - Client-side tools for managing networking resources.
- `Makefile` - Build and test automation.
- `go.mod` / `go.sum` - Dependency management.

## Testing Rules

- Unit test files should be named `<go_file>_test.go` and be in the same directory.
- Use table-driven tests for unit tests.
- Run `go test -v ./...` to execute all tests.
- Run tests for modified packages and verify they pass.
- Share analysis for failing tests and propose fixes.
- Integration test files should be named `<go_file>_integration_test.go` and placed in the same or `test` directory.
- Integration and e2e tests are written in Ginkgo style.
- E2E tests are under `test/e2e` and run with `make e2e-tests`.
- Clean up e2e tests with `make e2e-cleanup`.
- When adding tests, reuse existing setup and contexts where possible.
- Only add imports if absolutely needed.

## Domain Knowledge

Use the files in the `docs/**` and `.github/.copilot/domain_knowledge/**/*` as a source of truth when it comes to domain knowledge. These files provide context in which the current solution operates. This folder contains information like entity relationships, workflows, and ubiquitous language. As the understanding of the domain grows, take the opportunity to update these files as needed.

## Specification Files

Use specifications from the `.github/.copilot/specifications` folder. Each folder under `specifications` groups similar specifications together. Always ask the user which specifications best apply for the current conversation context if you're not sure.

Use the `.github/.copilot/specifications/.template.md` file as a template for specification structure.

   examples:
   ```text
   ├── application_architecture
   │   └── main.spec.md
   |   └── specific-feature.spec.md
   ├── database
   │   └── main.spec.md
   ├── observability
   │   └── main.spec.md
   └── testing
      └── main.spec.md
   ```

## Breadcrumb Protocol

A breadcrumb is a collaborative scratch pad that allow the user and agent to get alignment on context. When working on tasks in this repository, follow this collaborative documentation workflow to create a clear trail of decisions and implementations:

1. At the start of each new task, ask me for a breadcrumb file name if you can't determine a suitable one.

2. Create the breadcrumb file in the `${REPO}/.github/.copilot/breadcrumbs` folder using the format: `yyyy-mm-dd-HHMM-{title}.md` (*year-month-date-current_time_in-24hr_format-{title}.md* using UTC timezone)

3. Structure the breadcrumb file with these required sections:
   - **Requirements**: Clear list of what needs to be implemented.
   - **Additional comments from user**: Any additional input from the user during the conversation.
   - **Plan**: Strategy and technical plan before implementation.
   - **Decisions**: Why specific implementation choices were made.
   - **Implementation Details**: Code snippets with explanations for key files.
   - **Changes Made**: Summary of files modified and how they changed.
   - **Before/After Comparison**: Highlighting the improvements.
   - **References**: List of referred material like domain knowledge files, specification files, URLs and summary of what is was used for. If there is a version in the domain knowledge or in the specifications, record the version in the breadcrumb.

4. Workflow rules:
   - Update the breadcrumb **BEFORE** making any code changes.
   - **Get explicit approval** on the plan before implementation.
   - Update the breadcrumb **AFTER completing each significant change**.
   - Keep the breadcrumb as our single source of truth as it contains the most recent information.

5. Ask me to verify the plan with: "Are you happy with this implementation plan?" before proceeding with code changes.

6. Reference related breadcrumbs when a task builds on previous work.

7. Before concluding, ensure the breadcrumb file properly documents the entire process, including any course corrections or challenges encountered.

This practice creates a trail of decision points that document our thought process while building features in this solution, making pull request review for the current change easier to follow as well.

### Plan Structure Guidelines
- When creating a plan, organize it into numbered phases (e.g., "Phase 1: Setup Dependencies").
- Break down each phase into specific tasks with numeric identifiers (e.g., "Task 1.1: Add Dependencies").
- Include a detailed checklist at the end of the document that maps to all phases and tasks.
- Plan should always lookup related information from `domain_knowledge` folder and any applicable specification files.
- Mark tasks as `- [ ]` for pending tasks and `- [x]` for completed tasks.
- Start all planning tasks as unchecked, and update them to checked as implementation proceeds.
- Each planning task should have clear success criteria.
- End the plan with success criteria that define when the implementation is complete.
- Plans should start with writing Unit Tests first when possible, so we can use those to guide our implementation. Same for UI tests when it makes sense.
- If the domain knowledge has changed, update the related files in the `domain_knowledge` folder.
- If specifications have changed, update the related files in the `specifications` folder.

### Following Plans
- When coding you need to follow the plan phases and check off the tasks as they are completed.  
- As you complete a task, update the plan and mark that task complete before you begin the next task. 
- Tasks that involved tests should not be marked complete until the tests pass. 

### Example Usage

**User**: I need to implement JWT authentication for our Express API.

**Agent**: I'll help with implementing JWT authentication for your Express API. Let me create a breadcrumb file to track our implementation. What would you like to name this breadcrumb?

**User**: Use "jwt-auth-implementation"

**Agent**: I've created the breadcrumb file `2025-04-08-1430-jwt-auth-implementation.md`. Are you happy with this implementation plan?

[high level plan with tasks here]

**User**: Looks good, but let's use 30-minute expiration for tokens.

**Agent**: I've updated the breadcrumb with your specification: Are you happy with this implementation plan?

[high level plan with tasks here]

**User**: Yes, please proceed.

**Agent**: I'll implement the JWT authentication now. Let me start by installing the necessary packages and creating the authentication middleware. I've updated the breadcrumb with implementation details. Is there anything specific you'd like me to modify or explain about the implementation?

**User**: This looks good.

**Agent**: I've updated the breadcrumb with the latest understanding.