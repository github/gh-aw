---
# Chroma MCP Server
# Requires cache-memory with chroma cache ID for persistent storage

tools:
  cache-memory:
    - id: chroma
      key: memory-chroma-${{ github.workflow }}
      description: Persistent storage for Chroma vector database

mcp-servers:
  chroma:
    container: "mcp/chroma"
    env:
      CHROMA_CLIENT_TYPE: "persistent"
      CHROMA_DATA_DIR: "/tmp/gh-aw/cache-memory-chroma"
    allowed:
      - chroma_list_collections
      - chroma_create_collection
      - chroma_peek_collection
      - chroma_get_collection_info
      - chroma_get_collection_count
      - chroma_modify_collection
      - chroma_delete_collection
      - chroma_fork_collection
      - chroma_add_documents
      - chroma_query_documents
      - chroma_get_documents
      - chroma_update_documents
      - chroma_delete_documents
      - mcp_known_embedding_functions
---

<!--
## Chroma MCP Server

Provides the Chroma MCP server for vector database and semantic search capabilities with persistent storage using the cache-memory directory.

Chroma is an open-source embedding database that provides standardized, programmatic access to vector search and semantic search capabilities for AI agents.

### Available Tools

#### Collection Management (8 tools):
- **`chroma_list_collections`** - List all collections with pagination support
  - Parameters: `limit` (optional), `offset` (optional)
- **`chroma_create_collection`** - Create a new collection with embedding function and metadata
  - Parameters: `collection_name`, `embedding_function_name` (optional), `metadata` (optional)
- **`chroma_peek_collection`** - Preview sample documents from a collection
  - Parameters: `collection_name`, `limit` (optional)
- **`chroma_get_collection_info`** - Get collection information and statistics
  - Parameters: `collection_name`
- **`chroma_get_collection_count`** - Get the total number of documents in a collection
  - Parameters: `collection_name`
- **`chroma_modify_collection`** - Update a collection's name or metadata
  - Parameters: `collection_name`, `new_name` (optional), `metadata` (optional)
- **`chroma_delete_collection`** - Delete a collection
  - Parameters: `collection_name`
- **`chroma_fork_collection`** - Fork an existing collection to a new one
  - Parameters: `source_collection_name`, `new_collection_name`

#### Document Operations (5 tools):
- **`chroma_add_documents`** - Add new documents with IDs and optional metadata
  - Parameters: `collection_name`, `documents`, `ids`, `metadatas` (optional)
- **`chroma_query_documents`** - Query with semantic search and advanced filtering
  - Parameters: `collection_name`, `query`, `filters` (optional), `limit` (optional)
- **`chroma_get_documents`** - Retrieve documents by ID or with filters and pagination
  - Parameters: `collection_name`, `ids` (optional), `filter` (optional), `limit` (optional), `offset` (optional)
- **`chroma_update_documents`** - Update document content, metadata, or embeddings
  - Parameters: `collection_name`, `ids`, `new_documents` (optional), `new_metadata` (optional)
- **`chroma_delete_documents`** - Delete specific documents
  - Parameters: `collection_name`, `ids`

#### Additional Tools:
- **`mcp_known_embedding_functions`** - List available embedding functions for collections

### Setup

1. Import this configuration (cache-memory is automatically configured):
```yaml
imports:
  - shared/mcp/chroma.md
```

### Example Usage

```yaml
---
on: workflow_dispatch
engine: copilot
imports:
  - shared/mcp/chroma.md
---

# Semantic Search Workflow

Use Chroma to build a semantic search index of documentation and answer questions.

1. Create a collection called "docs"
2. Add documents with embeddings
3. Query for relevant information based on semantic similarity
4. The data persists across runs via cache-memory
```

### How It Works

The Chroma MCP server stores vector embeddings and metadata in `/tmp/gh-aw/cache-memory-chroma/`, which persists across workflow runs via GitHub Actions cache with cache ID "chroma". The server uses uvx to run the chroma-mcp-server package in persistent mode.

### Use Cases

- **Knowledge Base**: Build a searchable knowledge base from documents
- **Semantic Search**: Find similar documents or answers to questions
- **Document Clustering**: Group related documents by semantic similarity
- **Content Recommendations**: Find relevant content based on embeddings
- **Memory Augmentation**: Enhance AI agents with long-term semantic memory

### Configuration Options

The Chroma MCP server can be customized by modifying the configuration:

- **Client Type**: `persistent` (default, stores data) or `ephemeral` (in-memory only)
- **Data Directory**: `/tmp/gh-aw/cache-memory-chroma` (uses cache ID "chroma" for isolation)
- **Cache ID**: `chroma` (separate cache from other workflows)
- **Embedding Functions**: Multiple embedding functions available via `mcp_known_embedding_functions`

### Documentation

- **GitHub Repository**: https://github.com/chroma-core/chroma-mcp
- **PyPI Package**: https://pypi.org/project/chroma-mcp-server/
- **API Reference**: https://deepwiki.com/chroma-core/chroma-mcp/3-api-reference
- **Chroma Documentation**: https://docs.trychroma.com/

-->
