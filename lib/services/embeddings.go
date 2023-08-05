/*
 * Copyright 2023 Gravitational, Inc.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package services

import (
	"context"

	embeddingpb "github.com/gravitational/teleport/api/gen/proto/go/teleport/embedding/v1"
	"github.com/gravitational/teleport/api/internalutils/stream"
)

// Embeddings service is responsible for storing and retrieving embeddings in
// the backend. The backend acts as an embedding cache. Embeddings can be
// re-generated by an ai.Embedder.
type Embeddings interface {
	// GetEmbedding looks up a single embedding by its name in the backend.
	GetEmbedding(ctx context.Context, kind, resourceID string) (*embeddingpb.Embedding, error)
	// GetEmbeddings returns all embeddings for a given kind.
	GetEmbeddings(ctx context.Context, kind string) stream.Stream[*embeddingpb.Embedding]
	// UpsertEmbedding creates or updates a single ai.Embedding in the backend.
	UpsertEmbedding(ctx context.Context, embedding *embeddingpb.Embedding) (*embeddingpb.Embedding, error)
}
