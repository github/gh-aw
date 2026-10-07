---
services:
  jupyter:
    image: jupyter/base-notebook:latest@sha256:8c903974902b0e9d45d9823c2234411de0614c5c98c4bb782b3d4f55b3e435e6
    ports:
      - 8888:8888
    env:
      JUPYTER_TOKEN: "${{ secrets.JUPYTER_TOKEN || fromJSON('Missing required secret: JUPYTER_TOKEN') }}"
    options: >-
      --health-cmd "curl -f http://localhost:8888/api || exit 1"
      --health-interval 10s
      --health-timeout 5s
      --health-retries 5
  
  jupyter-mcp:
    image: datalayer/jupyter-mcp-server:2.2.3@sha256:889fcbddbc816612c657e8ac10b4434e5df8d6298125c17e763947baae6bc27a
    ports:
      - 3000:3000
    env:
      JUPYTER_URL: "http://jupyter:8888"
      JUPYTER_TOKEN: "${{ secrets.JUPYTER_TOKEN || fromJSON('Missing required secret: JUPYTER_TOKEN') }}"
      ALLOW_IMG_OUTPUT: "true"
      MCP_TRANSPORT: "http"
      MCP_PORT: "3000"
    options: >-
      --health-cmd "curl -f http://localhost:3000/health || exit 1"
      --health-interval 10s
      --health-timeout 5s
      --health-retries 5

mcp-servers:
  jupyter:
    type: http
    url: "http://jupyter-mcp:3000"
    # Security decision (2026-05-19): restrict to notebook creation/execution/read tools only.
    allowed:
      - execute_cell
      - get_cell_output
      - create_notebook
      - list_notebooks
      - get_notebook_content
---

<!--

# Jupyter Notebook MCP Server
# Manipulate Jupyter notebooks and execute code cells

Provides integration with Jupyter servers to run code cells, manage notebooks,
and visualize data using the Jupyter MCP Server.

Documentation: https://pypi.org/project/jupyter-mcp-server/

Available tools:
  - execute_cell: Execute code in a notebook cell
  - get_cell_output: Retrieve output from executed cells
  - create_notebook: Create new Jupyter notebooks
  - list_notebooks: List available notebooks
  - get_notebook_content: Read notebook contents

Configuration:
  The server connects to a Jupyter server instance using the provided URL and token.
  Set DOCUMENT_ID to specify the default notebook to work with.
  Enable ALLOW_IMG_OUTPUT to support image outputs from cells.

Setup:
  1. Start a Jupyter server locally or remotely
  2. Generate a Jupyter token for authentication
  3. Add the following secrets to your GitHub repository:
     - JUPYTER_TOKEN: Your Jupyter server authentication token

  4. Include in Your Workflow:
     imports:
       - shared/mcp/jupyter.md

Connection:
  The server connects to Jupyter via the JUPYTER_URL (default: http://host.docker.internal:8888)
  which allows Docker containers to access services running on the host machine.

Security:
  - Store the JUPYTER_TOKEN as a GitHub secret
  - Ensure your Jupyter server is properly secured
  - Consider network restrictions if running in production

Example Usage:
  Create a Jupyter notebook that analyzes repository data and generates visualizations.
  Execute Python code cells to process data and create charts.

Usage:
  imports:
    - shared/mcp/jupyter.md

-->
