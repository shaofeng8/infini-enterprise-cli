package storage

import (
	"encoding/json"
	"io"
	"net/url"
	"os"
	"slices"
	"strconv"

	"github.com/chaozwn/infini-enterprise-cli/internal/cliexit"
)

// A chunked upload is a five-step session: init, then a PUT per chunk, then
// complete — with status to check progress and cancel to abandon it.
//
// The reason to use it over a plain upload is resumability. The session
// remembers which chunks arrived, so an interrupted transfer picks up where it
// stopped instead of starting over, which matters for the multi-gigabyte
// archives these targets accept.
var (
	// TargetTypes are the resources an upload can land in.
	TargetTypes = []string{"project", "rag", "database", "task"}
	// PostActions decide what happens after the bytes are assembled.
	PostActions = []string{"store", "extract_archive", "build_rag", "import_database"}
	// ConflictPolicies decide what happens to a file of the same name.
	ConflictPolicies = []string{"overwrite", "skip", "rename"}
)

// DefaultChunkSize matches the server's default. The server accepts 1 MiB to
// 128 MiB.
const DefaultChunkSize = 8 << 20

// Session is the upload's state. UploadedChunks is the resumable part: it is
// the authoritative list of indexes the server already holds.
type Session struct {
	UploadID       string `json:"uploadId"`
	TargetType     string `json:"targetType"`
	TargetID       string `json:"targetId"`
	TargetPath     string `json:"targetPath"`
	FileName       string `json:"fileName"`
	FileSize       int64  `json:"fileSize"`
	ChunkSize      int    `json:"chunkSize"`
	TotalChunks    int    `json:"totalChunks"`
	UploadedChunks []int  `json:"uploadedChunks"`
	Status         string `json:"status"`
	PostAction     string `json:"postAction"`
	ConflictPolicy string `json:"conflictPolicy"`
	ExpiresAt      string `json:"expiresAt"`
	// Result is set only by complete, and carries whatever the post-action
	// produced — an imported data source, a built knowledge base, and so on.
	Result json.RawMessage `json:"result,omitempty"`
}

// Remaining lists the chunk indexes still to send.
func (s Session) Remaining() []int {
	remaining := make([]int, 0, s.TotalChunks-len(s.UploadedChunks))
	for index := range s.TotalChunks {
		if !slices.Contains(s.UploadedChunks, index) {
			remaining = append(remaining, index)
		}
	}
	return remaining
}

type InitRequest struct {
	TargetType     string `json:"targetType"`
	TargetID       string `json:"targetId"`
	TargetPath     string `json:"targetPath,omitempty"`
	FileName       string `json:"fileName"`
	FileSize       int64  `json:"fileSize"`
	MimeType       string `json:"mimeType,omitempty"`
	Checksum       string `json:"checksum,omitempty"`
	ChunkSize      int    `json:"chunkSize,omitempty"`
	PostAction     string `json:"postAction,omitempty"`
	ConflictPolicy string `json:"conflictPolicy,omitempty"`
}

func (a *API) Init(request InitRequest) (*Session, error) {
	raw, err := a.c.Post(uploadBase+"/init", request)
	session, err := decode[Session](raw, err, "upload session")
	if err != nil {
		return nil, err
	}
	return &session, nil
}

func (a *API) Status(uploadID string) (*Session, error) {
	raw, err := a.c.Get(uploadBase+"/"+url.PathEscape(uploadID)+"/status", nil)
	session, err := decode[Session](raw, err, "upload session")
	if err != nil {
		return nil, err
	}
	return &session, nil
}

// SendChunk PUTs one chunk's bytes as the entire request body.
func (a *API) SendChunk(uploadID string, index int, body io.Reader, length int64) (*Session, error) {
	path := uploadBase + "/" + url.PathEscape(uploadID) + "/chunks/" + strconv.Itoa(index)
	raw, err := a.c.Stream("PUT", path, body, length)
	session, err := decode[Session](raw, err, "upload session")
	if err != nil {
		return nil, err
	}
	return &session, nil
}

// Complete assembles the chunks and runs the post-action. It fails rather
// than assembling a partial file if any chunk is missing.
func (a *API) Complete(uploadID string) (*Session, error) {
	raw, err := a.c.Post(uploadBase+"/"+url.PathEscape(uploadID)+"/complete", nil)
	session, err := decode[Session](raw, err, "upload session")
	if err != nil {
		return nil, err
	}
	return &session, nil
}

func (a *API) Cancel(uploadID string) (json.RawMessage, error) {
	return a.c.Delete(uploadBase+"/"+url.PathEscape(uploadID), nil)
}

// Progress reports a chunk that finished, so a long transfer can say something
// while it runs.
type Progress func(session *Session, index int)

// SendFile drives the whole session for a local file.
//
// Chunks the server already holds are skipped, which is what makes a resumed
// transfer cheap. The file is read a chunk at a time with ReadAt rather than
// streamed, because a retry has to be able to re-read a chunk it already
// passed.
func (a *API) SendFile(session *Session, filePath string, onProgress Progress) (*Session, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, cliexit.Usage("cannot read %q: %v", filePath, err)
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return nil, cliexit.Usage("cannot inspect %q: %v", filePath, err)
	}
	if info.Size() != session.FileSize {
		return nil, cliexit.Usage(
			"%q is %d bytes but the session was opened for %d; the file changed under the upload",
			filePath, info.Size(), session.FileSize)
	}

	chunkSize := int64(session.ChunkSize)
	current := session
	for _, index := range session.Remaining() {
		offset := int64(index) * chunkSize
		length := min(chunkSize, info.Size()-offset)
		updated, err := a.SendChunk(session.UploadID, index,
			io.NewSectionReader(file, offset, length), length)
		if err != nil {
			return current, err
		}
		current = updated
		if onProgress != nil {
			onProgress(updated, index)
		}
	}
	return a.Complete(session.UploadID)
}
