// 文件说明：编排代码库索引流程。
// 实现原理：扫描文件、计算文件哈希快照，按变化文件切块和向量化，再通过 VectorStore 全量写入或按文件增量替换。
// 使用方式：CLI index 命令创建 Indexer 后调用 Index 方法。
// 注意事项：首次索引或旧快照缺少文件哈希时会执行全量重建；后续基于文件 hash 执行增量索引。
// 交互模块：internal/scanner、internal/splitter、internal/embed、internal/vectorstore、internal/snapshot。

package indexer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"sort"
	"strconv"

	"github.com/mazhan465/Agent-Memory/internal/domain"
	"github.com/mazhan465/Agent-Memory/internal/embed"
	"github.com/mazhan465/Agent-Memory/internal/scanner"
	"github.com/mazhan465/Agent-Memory/internal/snapshot"
	"github.com/mazhan465/Agent-Memory/internal/splitter"
	"github.com/mazhan465/Agent-Memory/internal/vectorstore"
)

// Stats 表示索引结果统计。
type Stats struct {
	Namespace     string
	Path          string
	IndexedFiles  int
	TotalChunks   int
	AddedFiles    int
	ModifiedFiles int
	RemovedFiles  int
	FullReindex   bool
}

// Indexer 负责执行完整索引流程。
type Indexer struct {
	scanner       *scanner.Scanner
	splitter      splitter.Splitter
	embedder      embed.Embedder
	vectorStore   vectorstore.VectorStore
	snapshotStore *snapshot.Store
}

type fileData struct {
	file    scanner.File
	content []byte
	hash    string
}

type indexChanges struct {
	added    []string
	modified []string
	removed  []string
}

// New 创建索引器。
func New(
	scanner *scanner.Scanner,
	splitter splitter.Splitter,
	embedder embed.Embedder,
	vectorStore vectorstore.VectorStore,
	snapshotStore *snapshot.Store,
) *Indexer {
	return &Indexer{
		scanner:       scanner,
		splitter:      splitter,
		embedder:      embedder,
		vectorStore:   vectorStore,
		snapshotStore: snapshotStore,
	}
}

// Index 索引指定代码库，优先基于文件 hash 执行增量更新。
func (i *Indexer) Index(ctx context.Context, rootPath string) (Stats, error) {
	namespace, absolutePath, err := NamespaceForPath(rootPath)
	if err != nil {
		return Stats{}, err
	}
	previousInfo, hasIncrementalSnapshot, err := i.incrementalSnapshot(namespace)
	if err != nil {
		return Stats{}, err
	}
	if err := i.markIndexing(namespace, absolutePath); err != nil {
		return Stats{}, err
	}

	files, err := i.scanner.Scan(absolutePath)
	if err != nil {
		i.saveFailure(namespace, absolutePath, err)
		return Stats{}, err
	}

	if !hasIncrementalSnapshot {
		fileDataList := readFileData(files)
		fileStates := fileStateMap(fileDataList)
		return i.indexFull(ctx, namespace, absolutePath, fileDataList, fileHashesFromStates(fileStates), fileStates)
	}
	return i.indexIncremental(ctx, namespace, absolutePath, previousInfo, files)
}

func (i *Indexer) markIndexing(namespace string, absolutePath string) error {
	return i.snapshotStore.Save(snapshot.Info{
		Path:      absolutePath,
		Namespace: namespace,
		Status:    snapshot.StatusIndexing,
	})
}

func (i *Indexer) incrementalSnapshot(namespace string) (snapshot.Info, bool, error) {
	info, err := i.snapshotStore.Get(namespace)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return snapshot.Info{}, false, nil
		}
		return snapshot.Info{}, false, err
	}
	hasFileSnapshot := len(info.FileStates) > 0 || len(info.FileHashes) > 0
	return info, hasFileSnapshot && info.Status == snapshot.StatusIndexed, nil
}

func (i *Indexer) indexFull(
	ctx context.Context,
	namespace string,
	absolutePath string,
	fileDataList []fileData,
	fileHashes map[string]string,
	fileStates map[string]snapshot.FileState,
) (Stats, error) {
	documents, err := i.buildDocuments(ctx, namespace, fileDataList)
	if err != nil {
		i.saveFailure(namespace, absolutePath, err)
		return Stats{}, err
	}
	if err := i.vectorStore.Put(ctx, namespace, documents); err != nil {
		i.saveFailure(namespace, absolutePath, err)
		return Stats{}, err
	}
	stats := Stats{
		Namespace:    namespace,
		Path:         absolutePath,
		IndexedFiles: len(fileHashes),
		TotalChunks:  len(documents),
		FullReindex:  true,
	}
	if err := i.saveSuccess(stats, fileHashes, fileStates); err != nil {
		return Stats{}, err
	}
	return stats, nil
}

func (i *Indexer) indexIncremental(
	ctx context.Context,
	namespace string,
	absolutePath string,
	previousInfo snapshot.Info,
	files []scanner.File,
) (Stats, error) {
	if len(previousInfo.FileStates) == 0 {
		return i.indexIncrementalFromHashes(ctx, namespace, absolutePath, previousInfo.FileHashes, files)
	}
	changes, fileStates, changedData := detectFileStateChanges(previousInfo.FileStates, files)
	return i.applyIncrementalChanges(ctx, namespace, absolutePath, changes, fileStates, changedData)
}

func (i *Indexer) indexIncrementalFromHashes(
	ctx context.Context,
	namespace string,
	absolutePath string,
	previousHashes map[string]string,
	files []scanner.File,
) (Stats, error) {
	fileDataList := readFileData(files)
	fileStates := fileStateMap(fileDataList)
	fileHashes := fileHashesFromStates(fileStates)
	changes := diffFileHashes(previousHashes, fileHashes)
	changedData := selectChangedFileData(fileDataList, changes.changedPaths())
	return i.applyIncrementalChanges(ctx, namespace, absolutePath, changes, fileStates, changedData)
}

func (i *Indexer) applyIncrementalChanges(
	ctx context.Context,
	namespace string,
	absolutePath string,
	changes indexChanges,
	fileStates map[string]snapshot.FileState,
	changedData []fileData,
) (Stats, error) {
	fileHashes := fileHashesFromStates(fileStates)
	if changes.empty() {
		return i.saveNoChangeStats(ctx, namespace, absolutePath, fileHashes, fileStates)
	}

	documents, err := i.buildDocuments(ctx, namespace, changedData)
	if err != nil {
		i.saveFailure(namespace, absolutePath, err)
		return Stats{}, err
	}
	if err := i.vectorStore.ReplaceFiles(ctx, namespace, changes.replacePaths(), documents); err != nil {
		i.saveFailure(namespace, absolutePath, err)
		return Stats{}, err
	}
	totalChunks, err := i.vectorStore.Count(ctx, namespace)
	if err != nil {
		i.saveFailure(namespace, absolutePath, err)
		return Stats{}, err
	}
	stats := Stats{
		Namespace:     namespace,
		Path:          absolutePath,
		IndexedFiles:  len(fileHashes),
		TotalChunks:   totalChunks,
		AddedFiles:    len(changes.added),
		ModifiedFiles: len(changes.modified),
		RemovedFiles:  len(changes.removed),
	}
	if err := i.saveSuccess(stats, fileHashes, fileStates); err != nil {
		return Stats{}, err
	}
	return stats, nil
}

func (i *Indexer) saveNoChangeStats(
	ctx context.Context,
	namespace string,
	absolutePath string,
	fileHashes map[string]string,
	fileStates map[string]snapshot.FileState,
) (Stats, error) {
	totalChunks, err := i.vectorStore.Count(ctx, namespace)
	if err != nil {
		i.saveFailure(namespace, absolutePath, err)
		return Stats{}, err
	}
	stats := Stats{
		Namespace:    namespace,
		Path:         absolutePath,
		IndexedFiles: len(fileHashes),
		TotalChunks:  totalChunks,
	}
	if err := i.saveSuccess(stats, fileHashes, fileStates); err != nil {
		return Stats{}, err
	}
	return stats, nil
}

func (i *Indexer) saveSuccess(
	stats Stats,
	fileHashes map[string]string,
	fileStates map[string]snapshot.FileState,
) error {
	return i.snapshotStore.Save(snapshot.Info{
		Path:         stats.Path,
		Namespace:    stats.Namespace,
		Status:       snapshot.StatusIndexed,
		IndexedFiles: stats.IndexedFiles,
		TotalChunks:  stats.TotalChunks,
		FileHashes:   maps.Clone(fileHashes),
		FileStates:   maps.Clone(fileStates),
	})
}

func (i *Indexer) buildDocuments(ctx context.Context, namespace string, fileDataList []fileData) ([]vectorstore.Document, error) {
	domainResolver := domain.NewDefaultDomainResolver()
	documents := make([]vectorstore.Document, 0)
	for _, data := range fileDataList {
		chunks := i.splitter.Split(data.file.AbsolutePath, data.content, languageFromExtension(data.file.Extension))
		if len(chunks) == 0 {
			continue
		}
		fileDocuments, err := i.buildFileDocuments(ctx, namespace, data.file, chunks, domainResolver)
		if err != nil {
			return nil, err
		}
		documents = append(documents, fileDocuments...)
	}
	return documents, nil
}

func (i *Indexer) buildFileDocuments(
	ctx context.Context,
	namespace string,
	file scanner.File,
	chunks []splitter.Chunk,
	domainResolver *domain.DomainResolver,
) ([]vectorstore.Document, error) {
	texts := make([]string, 0, len(chunks))
	for _, chunk := range chunks {
		texts = append(texts, chunk.Content)
	}
	vectors, err := i.embedder.EmbedBatch(ctx, texts)
	if err != nil {
		return nil, err
	}
	if len(vectors) != len(chunks) {
		return nil, errors.New("embedding count mismatch")
	}
	documents := make([]vectorstore.Document, 0, len(chunks))
	for chunkIndex, chunk := range chunks {
		document, err := vectorDocument(ctx, namespace, file, chunk, vectors[chunkIndex], domainResolver)
		if err != nil {
			return nil, err
		}
		documents = append(documents, document)
	}
	return documents, nil
}

func vectorDocument(
	ctx context.Context,
	namespace string,
	file scanner.File,
	chunk splitter.Chunk,
	vector []float32,
	domainResolver *domain.DomainResolver,
) (vectorstore.Document, error) {
	metadata := map[string]string{
		"absolute_path": filepath.ToSlash(file.AbsolutePath),
	}
	maps.Copy(metadata, chunk.Metadata)
	decision, err := domainResolver.Resolve(ctx, domain.Input{
		Query:          chunk.Content,
		FileExtensions: []string{file.Extension},
		Metadata: map[string]string{
			"relative_path": file.RelativePath,
		},
	})
	if err != nil {
		return vectorstore.Document{}, err
	}
	if decision.Domain != "" {
		metadata[vectorstore.MetadataDomainPath] = string(decision.Domain)
	}
	if decision.ExperienceKind != "" {
		metadata[vectorstore.MetadataExperienceKind] = string(decision.ExperienceKind)
	}
	return vectorstore.Document{
		ID:            chunkID(file.RelativePath, chunk.StartLine, chunk.EndLine, chunk.Content),
		Namespace:     namespace,
		Vector:        vector,
		Content:       chunk.Content,
		RelativePath:  file.RelativePath,
		StartLine:     chunk.StartLine,
		EndLine:       chunk.EndLine,
		FileExtension: file.Extension,
		Language:      chunk.Language,
		Metadata:      metadata,
	}, nil
}

func (i *Indexer) saveFailure(namespace string, absolutePath string, err error) {
	_ = i.snapshotStore.Save(snapshot.Info{
		Path:         absolutePath,
		Namespace:    namespace,
		Status:       snapshot.StatusFailed,
		ErrorMessage: err.Error(),
	})
}

func readFileData(files []scanner.File) []fileData {
	result := make([]fileData, 0, len(files))
	for _, file := range files {
		content, err := os.ReadFile(file.AbsolutePath)
		if err != nil {
			continue
		}
		result = append(result, fileData{
			file:    file,
			content: content,
			hash:    contentHash(content),
		})
	}
	return result
}

func fileStateMap(fileDataList []fileData) map[string]snapshot.FileState {
	states := make(map[string]snapshot.FileState, len(fileDataList))
	for _, data := range fileDataList {
		states[data.file.RelativePath] = fileState(data.file, data.hash)
	}
	return states
}

func scannedFileStateMap(files []scanner.File) map[string]snapshot.FileState {
	states := make(map[string]snapshot.FileState, len(files))
	for _, file := range files {
		states[file.RelativePath] = fileState(file, "")
	}
	return states
}

func fileState(file scanner.File, hash string) snapshot.FileState {
	return snapshot.FileState{
		Hash:            hash,
		Size:            file.Size,
		ModTimeUnixNano: file.ModTimeUnixNano,
	}
}

func fileHashesFromStates(states map[string]snapshot.FileState) map[string]string {
	hashes := make(map[string]string, len(states))
	for path, state := range states {
		if state.Hash != "" {
			hashes[path] = state.Hash
		}
	}
	return hashes
}

func detectFileStateChanges(previous map[string]snapshot.FileState, files []scanner.File) (indexChanges, map[string]snapshot.FileState, []fileData) {
	currentStates := scannedFileStateMap(files)
	changes := indexChanges{}
	readPaths := make([]string, 0)
	for path, currentState := range currentStates {
		previousState, ok := previous[path]
		if !ok {
			readPaths = append(readPaths, path)
			continue
		}
		if sameFileMetadata(previousState, currentState) && previousState.Hash != "" {
			currentState.Hash = previousState.Hash
			currentStates[path] = currentState
			continue
		}
		readPaths = append(readPaths, path)
	}
	for path := range previous {
		if _, ok := currentStates[path]; !ok {
			changes.removed = append(changes.removed, path)
		}
	}
	sort.Strings(readPaths)

	readDataByPath := make(map[string]fileData, len(readPaths))
	for _, data := range readFileData(selectFiles(files, readPaths)) {
		readDataByPath[data.file.RelativePath] = data
	}
	changedData := make([]fileData, 0, len(readDataByPath))
	for _, path := range readPaths {
		data, ok := readDataByPath[path]
		if !ok {
			if _, existed := previous[path]; existed {
				changes.removed = append(changes.removed, path)
			}
			delete(currentStates, path)
			continue
		}
		currentState := currentStates[path]
		currentState.Hash = data.hash
		currentStates[path] = currentState
		previousState, existed := previous[path]
		if !existed {
			changes.added = append(changes.added, path)
			changedData = append(changedData, data)
			continue
		}
		if previousState.Hash != data.hash {
			changes.modified = append(changes.modified, path)
			changedData = append(changedData, data)
		}
	}
	changes.sort()
	return changes, currentStates, changedData
}

func sameFileMetadata(left snapshot.FileState, right snapshot.FileState) bool {
	return left.Size == right.Size && left.ModTimeUnixNano == right.ModTimeUnixNano
}

func diffFileHashes(previous map[string]string, current map[string]string) indexChanges {
	changes := indexChanges{}
	for path, currentHash := range current {
		previousHash, ok := previous[path]
		switch {
		case !ok:
			changes.added = append(changes.added, path)
		case previousHash != currentHash:
			changes.modified = append(changes.modified, path)
		}
	}
	for path := range previous {
		if _, ok := current[path]; !ok {
			changes.removed = append(changes.removed, path)
		}
	}
	changes.sort()
	return changes
}

func (c *indexChanges) sort() {
	sort.Strings(c.added)
	sort.Strings(c.modified)
	sort.Strings(c.removed)
}

func (c indexChanges) empty() bool {
	return len(c.added) == 0 && len(c.modified) == 0 && len(c.removed) == 0
}

func (c indexChanges) changedPaths() []string {
	paths := make([]string, 0, len(c.added)+len(c.modified))
	paths = append(paths, c.added...)
	paths = append(paths, c.modified...)
	sort.Strings(paths)
	return paths
}

func (c indexChanges) replacePaths() []string {
	paths := make([]string, 0, len(c.added)+len(c.modified)+len(c.removed))
	paths = append(paths, c.added...)
	paths = append(paths, c.modified...)
	paths = append(paths, c.removed...)
	sort.Strings(paths)
	return paths
}

func selectChangedFileData(fileDataList []fileData, paths []string) []fileData {
	pathSet := make(map[string]struct{}, len(paths))
	for _, path := range paths {
		pathSet[path] = struct{}{}
	}
	result := make([]fileData, 0, len(paths))
	for _, data := range fileDataList {
		if _, ok := pathSet[data.file.RelativePath]; ok {
			result = append(result, data)
		}
	}
	return result
}

func selectFiles(files []scanner.File, paths []string) []scanner.File {
	pathSet := make(map[string]struct{}, len(paths))
	for _, path := range paths {
		pathSet[path] = struct{}{}
	}
	result := make([]scanner.File, 0, len(paths))
	for _, file := range files {
		if _, ok := pathSet[file.RelativePath]; ok {
			result = append(result, file)
		}
	}
	return result
}

func contentHash(content []byte) string {
	hash := sha256.Sum256(content)
	return hex.EncodeToString(hash[:])
}

func chunkID(relativePath string, startLine int, endLine int, content string) string {
	key := relativePath + ":" + strconv.Itoa(startLine) + ":" + strconv.Itoa(endLine) + ":" + content
	hash := sha256.Sum256([]byte(key))
	return "chunk_" + hex.EncodeToString(hash[:])[:16]
}

func languageFromExtension(extension string) string {
	switch extension {
	case ".go":
		return "go"
	case ".ts", ".tsx":
		return "typescript"
	case ".js", ".jsx":
		return "javascript"
	case ".py":
		return "python"
	case ".java":
		return "java"
	case ".cpp", ".cc", ".cxx", ".h", ".hpp":
		return "cpp"
	case ".md", ".markdown":
		return "markdown"
	default:
		return "text"
	}
}
