package store

import (
	"context"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

type Neo4jStore struct {
	driver neo4j.DriverWithContext
}

func NewNeo4jStore(ctx context.Context, uri, username, password string) (*Neo4jStore, error) {
	driver, err := neo4j.NewDriverWithContext(uri, neo4j.BasicAuth(username, password, ""))
	if err != nil {
		return nil, err
	}
	if err := driver.VerifyConnectivity(ctx); err != nil {
		return nil, err
	}
	return &Neo4jStore{driver: driver}, nil
}

func (s *Neo4jStore) Close(ctx context.Context) error {
	return s.driver.Close(ctx)
}

func (s *Neo4jStore) UpsertRepoFile(ctx context.Context, repoPath, filePath string) error {
	session := s.driver.NewSession(ctx, neo4j.SessionConfig{DatabaseName: "neo4j"})
	defer session.Close(ctx)

	_, err := session.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (any, error) {
		_, err := tx.Run(ctx, `
MERGE (r:Repository {path: $repoPath})
MERGE (f:File {path: $filePath})
MERGE (r)-[:CONTAINS]->(f)
`, map[string]any{
			"repoPath": repoPath,
			"filePath": filePath,
		})
		return nil, err
	})
	return err
}

func (s *Neo4jStore) LinkSessionToFile(ctx context.Context, sessionID, filePath string) error {
	session := s.driver.NewSession(ctx, neo4j.SessionConfig{DatabaseName: "neo4j"})
	defer session.Close(ctx)

	_, err := session.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (any, error) {
		_, err := tx.Run(ctx, `
MERGE (s:Session {id: $sessionId})
MERGE (f:File {path: $filePath})
MERGE (s)-[:TOUCHED]->(f)
`, map[string]any{
			"sessionId": sessionID,
			"filePath":  filePath,
		})
		return nil, err
	})
	return err
}

func (s *Neo4jStore) UpsertMemoryNode(ctx context.Context, id, kind, label string) error {
	session := s.driver.NewSession(ctx, neo4j.SessionConfig{DatabaseName: "neo4j"})
	defer session.Close(ctx)

	_, err := session.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (any, error) {
		_, err := tx.Run(ctx, `
MERGE (n:Memory {id: $id})
SET n.kind = $kind, n.label = $label
`, map[string]any{
			"id":    id,
			"kind":  kind,
			"label": label,
		})
		return nil, err
	})
	return err
}
