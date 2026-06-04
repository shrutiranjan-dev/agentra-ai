package store

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

type Neo4jStore struct {
	driver neo4j.DriverWithContext
}

type RepoGraphSummary struct {
	RepoPath             string           `json:"repoPath"`
	IndexedFiles         int              `json:"indexedFiles"`
	IndexedDirectories   int              `json:"indexedDirectories"`
	ImportEdges          int              `json:"importEdges"`
	ReferenceEdges       int              `json:"referenceEdges"`
	SymbolReferenceEdges int              `json:"symbolReferenceEdges"`
	SymbolCount          int              `json:"symbolCount"`
	TouchedFiles         []string         `json:"touchedFiles"`
	RelatedFiles         []string         `json:"relatedFiles"`
	RelatedFileMatches   []FileMatch      `json:"relatedFileMatches,omitempty"`
	RelatedMemoryMatch   []MemoryMatch    `json:"relatedMemoryMatches,omitempty"`
	RelatedSymbols       []SymbolMatch    `json:"relatedSymbols,omitempty"`
	ChildSessions        []string         `json:"childSessions"`
	LineageSessions      []string         `json:"lineageSessions"`
	RecentSubtasks       []SubtaskSummary `json:"recentSubtasks,omitempty"`
	Memories             []MemorySummary  `json:"memories"`
}

type SubtaskSummary struct {
	SessionID string `json:"sessionId"`
	Title     string `json:"title"`
	Summary   string `json:"summary"`
}

type MemorySummary struct {
	ID    string `json:"id"`
	Kind  string `json:"kind"`
	Label string `json:"label"`
}

type FileMetadata struct {
	Name      string
	Ext       string
	Directory string
	SizeBytes int64
}

type SymbolMetadata struct {
	Name string
	Kind string
	Line int
}

type SymbolMatch struct {
	FilePath string   `json:"filePath"`
	Name     string   `json:"name"`
	Kind     string   `json:"kind"`
	Line     int      `json:"line"`
	Score    int      `json:"score"`
	Reasons  []string `json:"reasons"`
}

type FileMatch struct {
	Path    string   `json:"path"`
	Score   int      `json:"score"`
	Reasons []string `json:"reasons"`
	Source  string   `json:"source,omitempty"`
}

type MemoryMatch struct {
	ID      string   `json:"id"`
	Kind    string   `json:"kind"`
	Label   string   `json:"label"`
	Score   int      `json:"score"`
	Reasons []string `json:"reasons"`
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

func (s *Neo4jStore) UpsertRepoFile(ctx context.Context, repoPath, filePath string, meta FileMetadata) error {
	session := s.driver.NewSession(ctx, neo4j.SessionConfig{DatabaseName: "neo4j"})
	defer session.Close(ctx)

	_, err := session.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (any, error) {
		_, err := tx.Run(ctx, `
MERGE (r:Repository {path: $repoPath})
MERGE (d:Directory {path: $directory})
SET d.name = $directoryName
MERGE (f:File {path: $filePath})
SET f.name = $name, f.ext = $ext, f.directory = $directory, f.sizeBytes = $sizeBytes
MERGE (r)-[:CONTAINS]->(d)
MERGE (d)-[:CONTAINS]->(f)
`, map[string]any{
			"repoPath":      repoPath,
			"filePath":      filePath,
			"name":          meta.Name,
			"ext":           meta.Ext,
			"directory":     meta.Directory,
			"directoryName": filepath.Base(meta.Directory),
			"sizeBytes":     meta.SizeBytes,
		})
		return nil, err
	})
	return err
}

func (s *Neo4jStore) UpsertFileImports(ctx context.Context, filePath string, imports []string) error {
	if len(imports) == 0 {
		return nil
	}
	session := s.driver.NewSession(ctx, neo4j.SessionConfig{DatabaseName: "neo4j"})
	defer session.Close(ctx)

	_, err := session.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (any, error) {
		for _, item := range imports {
			if strings.TrimSpace(item) == "" {
				continue
			}
			_, err := tx.Run(ctx, `
MERGE (f:File {path: $filePath})
MERGE (i:Import {value: $importValue})
MERGE (f)-[:IMPORTS]->(i)
`, map[string]any{
				"filePath":    filePath,
				"importValue": item,
			})
			if err != nil {
				return nil, err
			}
		}
		return nil, nil
	})
	return err
}

func (s *Neo4jStore) UpsertFileSymbols(ctx context.Context, filePath string, symbols []SymbolMetadata) error {
	if len(symbols) == 0 {
		return nil
	}
	session := s.driver.NewSession(ctx, neo4j.SessionConfig{DatabaseName: "neo4j"})
	defer session.Close(ctx)

	_, err := session.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (any, error) {
		for _, symbol := range symbols {
			if strings.TrimSpace(symbol.Name) == "" {
				continue
			}
			_, err := tx.Run(ctx, `
MERGE (f:File {path: $filePath})
MERGE (s:Symbol {filePath: $filePath, name: $name, line: $line})
SET s.kind = $kind
MERGE (f)-[:DECLARES]->(s)
`, map[string]any{
				"filePath": filePath,
				"name":     symbol.Name,
				"kind":     symbol.Kind,
				"line":     symbol.Line,
			})
			if err != nil {
				return nil, err
			}
		}
		return nil, nil
	})
	return err
}

func (s *Neo4jStore) LinkFileReference(ctx context.Context, fromFilePath, toFilePath, kind string) error {
	if strings.TrimSpace(fromFilePath) == "" || strings.TrimSpace(toFilePath) == "" || fromFilePath == toFilePath {
		return nil
	}
	session := s.driver.NewSession(ctx, neo4j.SessionConfig{DatabaseName: "neo4j"})
	defer session.Close(ctx)

	_, err := session.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (any, error) {
		_, err := tx.Run(ctx, `
MERGE (from:File {path: $fromFilePath})
MERGE (to:File {path: $toFilePath})
MERGE (from)-[r:REFERENCES {kind: $kind}]->(to)
`, map[string]any{
			"fromFilePath": fromFilePath,
			"toFilePath":   toFilePath,
			"kind":         kind,
		})
		return nil, err
	})
	return err
}

func (s *Neo4jStore) LinkSymbolReference(ctx context.Context, fromFilePath, fromName, fromKind string, fromLine int, toFilePath, toName, toKind string, toLine int, kind string) error {
	if strings.TrimSpace(fromFilePath) == "" || strings.TrimSpace(toFilePath) == "" {
		return nil
	}
	if fromLine <= 0 || toLine <= 0 {
		return nil
	}
	if strings.TrimSpace(fromName) == "" {
		fromName = fmt.Sprintf("symbol_%d", fromLine)
	}
	if strings.TrimSpace(toName) == "" {
		toName = fmt.Sprintf("symbol_%d", toLine)
	}
	session := s.driver.NewSession(ctx, neo4j.SessionConfig{DatabaseName: "neo4j"})
	defer session.Close(ctx)

	_, err := session.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (any, error) {
		_, err := tx.Run(ctx, `
MERGE (fromFile:File {path: $fromFilePath})
MERGE (from:Symbol {filePath: $fromFilePath, name: $fromName, line: $fromLine})
SET from.kind = CASE WHEN $fromKind <> "" THEN $fromKind ELSE coalesce(from.kind, "") END
MERGE (fromFile)-[:DECLARES]->(from)
MERGE (toFile:File {path: $toFilePath})
MERGE (to:Symbol {filePath: $toFilePath, name: $toName, line: $toLine})
SET to.kind = CASE WHEN $toKind <> "" THEN $toKind ELSE coalesce(to.kind, "") END
MERGE (toFile)-[:DECLARES]->(to)
MERGE (from)-[:REFERS_TO {kind: $kind}]->(to)
`, map[string]any{
			"fromFilePath": fromFilePath,
			"fromName":     fromName,
			"fromKind":     fromKind,
			"fromLine":     fromLine,
			"toFilePath":   toFilePath,
			"toName":       toName,
			"toKind":       toKind,
			"toLine":       toLine,
			"kind":         kind,
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

func (s *Neo4jStore) UpsertSessionNode(ctx context.Context, sessionID, title string) error {
	session := s.driver.NewSession(ctx, neo4j.SessionConfig{DatabaseName: "neo4j"})
	defer session.Close(ctx)

	_, err := session.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (any, error) {
		_, err := tx.Run(ctx, `
MERGE (s:Session {id: $sessionId})
SET s.title = $title
`, map[string]any{
			"sessionId": sessionID,
			"title":     title,
		})
		return nil, err
	})
	return err
}

func (s *Neo4jStore) LinkChildSession(ctx context.Context, parentSessionID, childSessionID string) error {
	if strings.TrimSpace(parentSessionID) == "" || strings.TrimSpace(childSessionID) == "" {
		return nil
	}
	session := s.driver.NewSession(ctx, neo4j.SessionConfig{DatabaseName: "neo4j"})
	defer session.Close(ctx)

	_, err := session.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (any, error) {
		_, err := tx.Run(ctx, `
MERGE (parent:Session {id: $parentSessionId})
MERGE (child:Session {id: $childSessionId})
MERGE (child)-[:CHILD_OF]->(parent)
`, map[string]any{
			"parentSessionId": parentSessionID,
			"childSessionId":  childSessionID,
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

func (s *Neo4jStore) LinkMemoryToSession(ctx context.Context, memoryID, sessionID string) error {
	if strings.TrimSpace(memoryID) == "" || strings.TrimSpace(sessionID) == "" {
		return nil
	}
	session := s.driver.NewSession(ctx, neo4j.SessionConfig{DatabaseName: "neo4j"})
	defer session.Close(ctx)

	_, err := session.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (any, error) {
		_, err := tx.Run(ctx, `
MERGE (m:Memory {id: $memoryId})
MERGE (s:Session {id: $sessionId})
MERGE (s)-[:REMEMBERS]->(m)
`, map[string]any{
			"memoryId":  memoryID,
			"sessionId": sessionID,
		})
		return nil, err
	})
	return err
}

func (s *Neo4jStore) RepoGraphSummary(ctx context.Context, repoPath string, sessionID string) (RepoGraphSummary, error) {
	session := s.driver.NewSession(ctx, neo4j.SessionConfig{DatabaseName: "neo4j"})
	defer session.Close(ctx)

	summary := RepoGraphSummary{
		RepoPath:           repoPath,
		TouchedFiles:       []string{},
		RelatedFiles:       []string{},
		RelatedFileMatches: []FileMatch{},
		RelatedMemoryMatch: []MemoryMatch{},
		RelatedSymbols:     []SymbolMatch{},
		ChildSessions:      []string{},
		LineageSessions:    []string{},
		RecentSubtasks:     []SubtaskSummary{},
		Memories:           []MemorySummary{},
	}

	statsResult, err := session.ExecuteRead(ctx, func(tx neo4j.ManagedTransaction) (any, error) {
		result, err := tx.Run(ctx, `
OPTIONAL MATCH (r:Repository {path: $repoPath})-[:CONTAINS]->(d:Directory)
OPTIONAL MATCH (d)-[:CONTAINS]->(f:File)
OPTIONAL MATCH (f)-[imp:IMPORTS]->(:Import)
OPTIONAL MATCH (f)-[:DECLARES]->(sym:Symbol)
OPTIONAL MATCH (f)-[ref:REFERENCES]->(:File)
OPTIONAL MATCH (sym)-[symref:REFERS_TO]->(:Symbol)
RETURN count(DISTINCT d) AS directories, count(DISTINCT f) AS files, count(DISTINCT imp) AS imports, count(DISTINCT ref) AS references, count(DISTINCT symref) AS symbolReferences, count(DISTINCT sym) AS symbols, collect(DISTINCT f.path)[0..20] AS filePaths
`, map[string]any{"repoPath": repoPath})
		if err != nil {
			return nil, err
		}
		if result.Next(ctx) {
			return result.Record().Values, nil
		}
		return []any{int64(0), int64(0), int64(0), int64(0), int64(0), int64(0), []any{}}, result.Err()
	})
	if err != nil {
		return summary, err
	}
	if values, ok := statsResult.([]any); ok && len(values) >= 7 {
		summary.IndexedDirectories = intFromAny(values[0])
		summary.IndexedFiles = intFromAny(values[1])
		summary.ImportEdges = intFromAny(values[2])
		summary.ReferenceEdges = intFromAny(values[3])
		summary.SymbolReferenceEdges = intFromAny(values[4])
		summary.SymbolCount = intFromAny(values[5])
		summary.TouchedFiles = append(summary.TouchedFiles, stringSliceFromAny(values[6])...)
	}

	memoriesResult, err := session.ExecuteRead(ctx, func(tx neo4j.ManagedTransaction) (any, error) {
		query := `
MATCH (m:Memory)
RETURN m.id AS id, m.kind AS kind, m.label AS label
LIMIT 10
`
		params := map[string]any{}
		if strings.TrimSpace(sessionID) != "" {
			query = `
MATCH (root:Session {id: $sessionId})
OPTIONAL MATCH (root)-[:CHILD_OF*0..4]->(ancestor:Session)
OPTIONAL MATCH (descendant:Session)-[:CHILD_OF*1..4]->(root)
WITH collect(DISTINCT root) + collect(DISTINCT ancestor) + collect(DISTINCT descendant) AS sessions
UNWIND sessions AS session
MATCH (session)-[:REMEMBERS]->(m:Memory)
RETURN DISTINCT m.id AS id, m.kind AS kind, m.label AS label
LIMIT 10
`
			params["sessionId"] = sessionID
		}
		result, err := tx.Run(ctx, query, params)
		if err != nil {
			return nil, err
		}
		items := make([]MemorySummary, 0)
		for result.Next(ctx) {
			record := result.Record()
			id, _ := record.Get("id")
			kind, _ := record.Get("kind")
			label, _ := record.Get("label")
			items = append(items, MemorySummary{
				ID:    stringifyValue(id),
				Kind:  stringifyValue(kind),
				Label: stringifyValue(label),
			})
		}
		return items, result.Err()
	})
	if err != nil {
		return summary, err
	}
	if items, ok := memoriesResult.([]MemorySummary); ok {
		summary.Memories = items
	}

	childSessionsResult, err := session.ExecuteRead(ctx, func(tx neo4j.ManagedTransaction) (any, error) {
		result, err := tx.Run(ctx, `
MATCH (child:Session)-[:CHILD_OF]->(parent:Session)
RETURN child.title AS title
LIMIT 10
`, map[string]any{})
		if err != nil {
			return nil, err
		}
		items := make([]string, 0)
		for result.Next(ctx) {
			record := result.Record()
			title, _ := record.Get("title")
			if value := stringifyValue(title); value != "" {
				items = append(items, value)
			}
		}
		return items, result.Err()
	})
	if err != nil {
		return summary, err
	}
	if items, ok := childSessionsResult.([]string); ok {
		summary.ChildSessions = items
	}
	lineageSessionsResult, err := session.ExecuteRead(ctx, func(tx neo4j.ManagedTransaction) (any, error) {
		if strings.TrimSpace(sessionID) == "" {
			return []string{}, nil
		}
		result, err := tx.Run(ctx, `
MATCH (root:Session {id: $sessionId})
OPTIONAL MATCH (root)-[:CHILD_OF*0..4]->(ancestor:Session)
OPTIONAL MATCH (descendant:Session)-[:CHILD_OF*1..4]->(root)
WITH collect(DISTINCT root) + collect(DISTINCT ancestor) + collect(DISTINCT descendant) AS sessions
UNWIND sessions AS session
RETURN DISTINCT coalesce(session.title, session.id) AS title
LIMIT 12
`, map[string]any{"sessionId": sessionID})
		if err != nil {
			return nil, err
		}
		items := make([]string, 0)
		for result.Next(ctx) {
			record := result.Record()
			title, _ := record.Get("title")
			if value := stringifyValue(title); value != "" {
				items = append(items, value)
			}
		}
		return items, result.Err()
	})
	if err != nil {
		return summary, err
	}
	if items, ok := lineageSessionsResult.([]string); ok {
		summary.LineageSessions = items
	}
	subtasksResult, err := session.ExecuteRead(ctx, func(tx neo4j.ManagedTransaction) (any, error) {
		query := `
MATCH (session:Session)-[:REMEMBERS]->(m:Memory)
WHERE m.kind = 'subtask'
RETURN session.id AS sessionId, coalesce(session.title, session.id) AS title, m.label AS summary
LIMIT 8
`
		params := map[string]any{}
		if strings.TrimSpace(sessionID) != "" {
			query = `
MATCH (root:Session {id: $sessionId})
OPTIONAL MATCH (root)-[:CHILD_OF*0..4]->(ancestor:Session)
OPTIONAL MATCH (descendant:Session)-[:CHILD_OF*1..4]->(root)
WITH collect(DISTINCT root) + collect(DISTINCT ancestor) + collect(DISTINCT descendant) AS sessions
UNWIND sessions AS session
MATCH (session)-[:REMEMBERS]->(m:Memory)
WHERE m.kind = 'subtask'
RETURN DISTINCT session.id AS sessionId, coalesce(session.title, session.id) AS title, m.label AS summary
LIMIT 8
`
			params["sessionId"] = sessionID
		}
		result, err := tx.Run(ctx, query, params)
		if err != nil {
			return nil, err
		}
		items := make([]SubtaskSummary, 0)
		for result.Next(ctx) {
			record := result.Record()
			sessionValue, _ := record.Get("sessionId")
			titleValue, _ := record.Get("title")
			summaryValue, _ := record.Get("summary")
			items = append(items, SubtaskSummary{
				SessionID: stringifyValue(sessionValue),
				Title:     stringifyValue(titleValue),
				Summary:   stringifyValue(summaryValue),
			})
		}
		return items, result.Err()
	})
	if err != nil {
		return summary, err
	}
	if items, ok := subtasksResult.([]SubtaskSummary); ok {
		summary.RecentSubtasks = items
	}
	sort.Strings(summary.TouchedFiles)
	return summary, nil
}

func (s *Neo4jStore) RelevantMemorySummaries(ctx context.Context, sessionID string, tokens []string, limit int) ([]MemoryMatch, error) {
	session := s.driver.NewSession(ctx, neo4j.SessionConfig{DatabaseName: "neo4j"})
	defer session.Close(ctx)
	if strings.TrimSpace(sessionID) == "" {
		return []MemoryMatch{}, nil
	}
	if limit <= 0 {
		limit = 8
	}
	result, err := session.ExecuteRead(ctx, func(tx neo4j.ManagedTransaction) (any, error) {
		rows, err := tx.Run(ctx, `
MATCH (root:Session {id: $sessionId})
OPTIONAL MATCH (root)-[:CHILD_OF*0..4]->(ancestor:Session)
OPTIONAL MATCH (descendant:Session)-[:CHILD_OF*1..4]->(root)
WITH collect(DISTINCT root) + collect(DISTINCT ancestor) + collect(DISTINCT descendant) AS sessions
UNWIND sessions AS session
MATCH (session)-[:REMEMBERS]->(m:Memory)
WITH m,
reduce(score = 0, token IN $tokens |
  score +
  CASE WHEN toLower(m.label) CONTAINS token THEN 5 ELSE 0 END +
  CASE WHEN toLower(m.kind) CONTAINS token THEN 2 ELSE 0 END
) AS score
WHERE score > 0
RETURN DISTINCT m.id AS id, m.kind AS kind, m.label AS label, score
ORDER BY score DESC, m.label ASC
LIMIT $limit
`, map[string]any{
			"sessionId": sessionID,
			"tokens":    tokens,
			"limit":     limit,
		})
		if err != nil {
			return nil, err
		}
		items := make([]MemoryMatch, 0)
		for rows.Next(ctx) {
			record := rows.Record()
			id, _ := record.Get("id")
			kind, _ := record.Get("kind")
			label, _ := record.Get("label")
			score, _ := record.Get("score")
			items = append(items, MemoryMatch{
				ID:      stringifyValue(id),
				Kind:    stringifyValue(kind),
				Label:   stringifyValue(label),
				Score:   intFromAny(score),
				Reasons: memoryReasons(stringifyValue(kind), stringifyValue(label), tokens),
			})
		}
		return items, rows.Err()
	})
	if err != nil {
		return nil, err
	}
	if items, ok := result.([]MemoryMatch); ok {
		return items, nil
	}
	return []MemoryMatch{}, nil
}

func (s *Neo4jStore) RelevantTouchedFiles(ctx context.Context, sessionID string, repoPath string, tokens []string, limit int) ([]FileMatch, error) {
	session := s.driver.NewSession(ctx, neo4j.SessionConfig{DatabaseName: "neo4j"})
	defer session.Close(ctx)
	if strings.TrimSpace(sessionID) == "" {
		return []FileMatch{}, nil
	}
	if limit <= 0 {
		limit = 8
	}
	result, err := session.ExecuteRead(ctx, func(tx neo4j.ManagedTransaction) (any, error) {
		rows, err := tx.Run(ctx, `
MATCH (root:Session {id: $sessionId})
OPTIONAL MATCH (root)-[:CHILD_OF*0..4]->(ancestor:Session)
OPTIONAL MATCH (descendant:Session)-[:CHILD_OF*1..4]->(root)
WITH collect(DISTINCT root) + collect(DISTINCT ancestor) + collect(DISTINCT descendant) AS sessions
UNWIND sessions AS session
MATCH (session)-[:TOUCHED]->(f:File)
WHERE startsWith(f.path, $repoPath)
OPTIONAL MATCH (f)-[outRef:REFERENCES]->(:File)
OPTIONAL MATCH (:File)-[inRef:REFERENCES]->(f)
OPTIONAL MATCH (f)-[:DECLARES]->(decl:Symbol)
OPTIONAL MATCH (decl)-[outSymRef:REFERS_TO]->(:Symbol)
OPTIONAL MATCH (:Symbol)-[inSymRef:REFERS_TO]->(decl)
WITH f,
reduce(score = 0, token IN $tokens |
  score +
  CASE WHEN toLower(f.path) CONTAINS token THEN 3 ELSE 0 END +
  CASE WHEN toLower(f.name) CONTAINS token THEN 5 ELSE 0 END
) + count(DISTINCT outRef) + count(DISTINCT inRef) + count(DISTINCT outSymRef) + count(DISTINCT inSymRef) AS score,
count(DISTINCT outRef) AS outRefs,
count(DISTINCT inRef) AS inRefs,
count(DISTINCT outSymRef) AS outSymRefs,
count(DISTINCT inSymRef) AS inSymRefs
WHERE score > 0
RETURN DISTINCT f.path AS path, score, outRefs, inRefs, outSymRefs, inSymRefs
ORDER BY score DESC, f.path ASC
LIMIT $limit
`, map[string]any{
			"sessionId": sessionID,
			"repoPath":  repoPath,
			"tokens":    tokens,
			"limit":     limit,
		})
		if err != nil {
			return nil, err
		}
		items := make([]FileMatch, 0)
		for rows.Next(ctx) {
			record := rows.Record()
			path, _ := record.Get("path")
			score, _ := record.Get("score")
			outRefs, _ := record.Get("outRefs")
			inRefs, _ := record.Get("inRefs")
			outSymRefs, _ := record.Get("outSymRefs")
			inSymRefs, _ := record.Get("inSymRefs")
			items = append(items, FileMatch{
				Path:    stringifyValue(path),
				Score:   intFromAny(score),
				Reasons: fileReasons(stringifyValue(path), tokens, intFromAny(outRefs), intFromAny(inRefs), intFromAny(outSymRefs), intFromAny(inSymRefs), true),
				Source:  "session-lineage",
			})
		}
		return items, rows.Err()
	})
	if err != nil {
		return nil, err
	}
	if items, ok := result.([]FileMatch); ok {
		return items, nil
	}
	return []FileMatch{}, nil
}

func (s *Neo4jStore) RelevantFiles(ctx context.Context, repoPath string, tokens []string, limit int) ([]FileMatch, error) {
	session := s.driver.NewSession(ctx, neo4j.SessionConfig{DatabaseName: "neo4j"})
	defer session.Close(ctx)
	if limit <= 0 {
		limit = 8
	}

	result, err := session.ExecuteRead(ctx, func(tx neo4j.ManagedTransaction) (any, error) {
		query := `
MATCH (r:Repository {path: $repoPath})-[:CONTAINS]->(:Directory)-[:CONTAINS]->(f:File)
OPTIONAL MATCH (f)-[:DECLARES]->(sym:Symbol)
OPTIONAL MATCH (f)-[outRef:REFERENCES]->(:File)
OPTIONAL MATCH (:File)-[inRef:REFERENCES]->(f)
OPTIONAL MATCH (sym)-[outSymRef:REFERS_TO]->(:Symbol)
OPTIONAL MATCH (:Symbol)-[inSymRef:REFERS_TO]->(sym)
WITH f, collect(toLower(sym.name)) AS symbolNames, count(DISTINCT outRef) AS outRefs, count(DISTINCT inRef) AS inRefs, count(DISTINCT outSymRef) AS outSymRefs, count(DISTINCT inSymRef) AS inSymRefs,
reduce(score = 0, token IN $tokens |
  score +
  CASE
    WHEN toLower(f.path) CONTAINS token THEN 3
    ELSE 0
  END +
  CASE
    WHEN toLower(f.name) CONTAINS token THEN 5
    ELSE 0
  END +
  CASE
    WHEN toLower(f.ext) = token THEN 2
    ELSE 0
  END +
  CASE
    WHEN any(symbolName IN symbolNames WHERE symbolName CONTAINS token) THEN 6
    ELSE 0
  END
) + count(DISTINCT outRef) + count(DISTINCT inRef) + count(DISTINCT outSymRef) + count(DISTINCT inSymRef) AS score
WHERE score > 0
RETURN f.path AS path, score, outRefs, inRefs, outSymRefs, inSymRefs
ORDER BY score DESC, f.path ASC
LIMIT $limit
`
		rows, err := tx.Run(ctx, query, map[string]any{
			"repoPath": repoPath,
			"tokens":   tokens,
			"limit":    limit,
		})
		if err != nil {
			return nil, err
		}
		items := make([]FileMatch, 0)
		for rows.Next(ctx) {
			record := rows.Record()
			path, _ := record.Get("path")
			score, _ := record.Get("score")
			outRefs, _ := record.Get("outRefs")
			inRefs, _ := record.Get("inRefs")
			outSymRefs, _ := record.Get("outSymRefs")
			inSymRefs, _ := record.Get("inSymRefs")
			items = append(items, FileMatch{
				Path:    stringifyValue(path),
				Score:   intFromAny(score),
				Reasons: fileReasons(stringifyValue(path), tokens, intFromAny(outRefs), intFromAny(inRefs), intFromAny(outSymRefs), intFromAny(inSymRefs), false),
				Source:  "repo-graph",
			})
		}
		return items, rows.Err()
	})
	if err != nil {
		return nil, err
	}
	if items, ok := result.([]FileMatch); ok {
		return items, nil
	}
	return []FileMatch{}, nil
}

func (s *Neo4jStore) RelevantSymbols(ctx context.Context, repoPath string, tokens []string, limit int) ([]SymbolMatch, error) {
	session := s.driver.NewSession(ctx, neo4j.SessionConfig{DatabaseName: "neo4j"})
	defer session.Close(ctx)
	if limit <= 0 {
		limit = 10
	}
	result, err := session.ExecuteRead(ctx, func(tx neo4j.ManagedTransaction) (any, error) {
		query := `
MATCH (r:Repository {path: $repoPath})-[:CONTAINS]->(:Directory)-[:CONTAINS]->(f:File)-[:DECLARES]->(sym:Symbol)
OPTIONAL MATCH (sym)-[outRef:REFERS_TO]->(:Symbol)
OPTIONAL MATCH (:Symbol)-[inRef:REFERS_TO]->(sym)
WITH f, sym, count(DISTINCT outRef) AS outRefs, count(DISTINCT inRef) AS inRefs,
reduce(score = 0, token IN $tokens |
  score +
  CASE WHEN toLower(sym.name) CONTAINS token THEN 8 ELSE 0 END +
  CASE WHEN toLower(sym.kind) CONTAINS token THEN 2 ELSE 0 END +
  CASE WHEN toLower(f.path) CONTAINS token THEN 2 ELSE 0 END
) + count(DISTINCT outRef) + count(DISTINCT inRef) AS score
WHERE score > 0
RETURN f.path AS filePath, sym.name AS name, sym.kind AS kind, sym.line AS line, score, outRefs, inRefs
ORDER BY score DESC, f.path ASC, sym.line ASC
LIMIT $limit
`
		rows, err := tx.Run(ctx, query, map[string]any{
			"repoPath": repoPath,
			"tokens":   tokens,
			"limit":    limit,
		})
		if err != nil {
			return nil, err
		}
		items := make([]SymbolMatch, 0)
		for rows.Next(ctx) {
			record := rows.Record()
			filePath, _ := record.Get("filePath")
			name, _ := record.Get("name")
			kind, _ := record.Get("kind")
			line, _ := record.Get("line")
			score, _ := record.Get("score")
			outRefs, _ := record.Get("outRefs")
			inRefs, _ := record.Get("inRefs")
			items = append(items, SymbolMatch{
				FilePath: stringifyValue(filePath),
				Name:     stringifyValue(name),
				Kind:     stringifyValue(kind),
				Line:     intFromAny(line),
				Score:    intFromAny(score),
				Reasons:  symbolReasons(stringifyValue(name), stringifyValue(kind), stringifyValue(filePath), tokens, intFromAny(outRefs), intFromAny(inRefs)),
			})
		}
		return items, rows.Err()
	})
	if err != nil {
		return nil, err
	}
	if items, ok := result.([]SymbolMatch); ok {
		return items, nil
	}
	return []SymbolMatch{}, nil
}

func intFromAny(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	default:
		return 0
	}
}

func stringSliceFromAny(value any) []string {
	items, ok := value.([]any)
	if !ok {
		return []string{}
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		if text := stringifyValue(item); text != "" {
			out = append(out, text)
		}
	}
	return out
}

func stringifyValue(value any) string {
	if value == nil {
		return ""
	}
	text, _ := value.(string)
	return text
}

func fileReasons(path string, tokens []string, outRefs, inRefs, outSymRefs, inSymRefs int, touched bool) []string {
	reasons := make([]string, 0, 6)
	loweredPath := strings.ToLower(path)
	base := strings.ToLower(filepath.Base(path))
	for _, token := range tokens {
		switch {
		case strings.Contains(base, token):
			reasons = append(reasons, "filename matches "+token)
		case strings.Contains(loweredPath, token):
			reasons = append(reasons, "path mentions "+token)
		}
	}
	if touched {
		reasons = append(reasons, "touched in session lineage")
	}
	if outRefs > 0 || inRefs > 0 {
		reasons = append(reasons, "connected by reference graph")
	}
	if outSymRefs > 0 || inSymRefs > 0 {
		reasons = append(reasons, "connected by symbol graph")
	}
	return uniqueReasonList(reasons)
}

func memoryReasons(kind, label string, tokens []string) []string {
	reasons := make([]string, 0, 4)
	loweredKind := strings.ToLower(kind)
	loweredLabel := strings.ToLower(label)
	for _, token := range tokens {
		switch {
		case strings.Contains(loweredLabel, token):
			reasons = append(reasons, "memory mentions "+token)
		case strings.Contains(loweredKind, token):
			reasons = append(reasons, "memory kind matches "+token)
		}
	}
	return uniqueReasonList(reasons)
}

func symbolReasons(name, kind, path string, tokens []string, outRefs, inRefs int) []string {
	reasons := make([]string, 0, 5)
	loweredName := strings.ToLower(name)
	loweredKind := strings.ToLower(kind)
	loweredPath := strings.ToLower(path)
	for _, token := range tokens {
		switch {
		case strings.Contains(loweredName, token):
			reasons = append(reasons, "symbol name matches "+token)
		case strings.Contains(loweredKind, token):
			reasons = append(reasons, "symbol kind matches "+token)
		case strings.Contains(loweredPath, token):
			reasons = append(reasons, "file path mentions "+token)
		}
	}
	if outRefs > 0 || inRefs > 0 {
		reasons = append(reasons, "linked in symbol reference graph")
	}
	return uniqueReasonList(reasons)
}

func uniqueReasonList(items []string) []string {
	seen := make(map[string]struct{}, len(items))
	result := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		result = append(result, item)
	}
	return result
}
