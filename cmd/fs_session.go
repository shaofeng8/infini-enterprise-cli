package cmd

import (
	"io"
	"mime"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/chaozwn/infini-enterprise-cli/internal/cliexit"
	"github.com/chaozwn/infini-enterprise-cli/internal/output"
	"github.com/chaozwn/infini-enterprise-cli/internal/storage"
	"github.com/spf13/cobra"
)

func requireChoice(flag, value string, allowed []string) error {
	if slices.Contains(allowed, value) {
		return nil
	}
	return cliexit.Usage("%s expects one of: %s, received %q",
		flag, strings.Join(allowed, ", "), value)
}

var fsSessionCmd = &cobra.Command{
	Use:   "session",
	Short: "Resumable chunked uploads",
	Long: `A session splits a file into chunks the server acknowledges one at a time, so
an interrupted transfer resumes instead of restarting. That is the whole
reason it exists — for anything that fits comfortably in one request,
` + "`fs put`" + ` is simpler.

The session also decides what happens to the bytes once they are assembled:
store them, extract an archive, build a knowledge base, or import a data
source. So this is the path for "load this 4 GB dump into a data source", not
just for moving a file.

` + "`push`" + ` runs the whole flow. The individual steps are there for when you
need to drive it yourself:

  init → chunks → complete       with status to check, cancel to abandon`,
}

var fsSessionPushCmd = &cobra.Command{
	Use:   "push <file>",
	Short: "Upload a file as a session, start to finish",
	Long: `  infini-cli fs session push ./dump.csv --target-type database --target-id db_1 \
      --post-action import_database
  infini-cli fs session push ./docs.zip --target-type rag --target-id rag_1 \
      --post-action build_rag

To resume an interrupted transfer, pass its id instead of starting over:

  infini-cli fs session push ./dump.csv --resume <uploadId>

Only the chunks the server is missing are sent, so resuming a mostly-finished
transfer costs almost nothing.`,
	Args: exactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		flags := cmd.Flags()
		resume, _ := flags.GetString("resume")

		// The request is built before the client so a bad target type reports
		// itself rather than an unconfigured server.
		var request *storage.InitRequest
		if resume == "" {
			built, err := initRequest(cmd, args[0])
			if err != nil {
				return err
			}
			request = built
		}

		api, err := storageAPI()
		if err != nil {
			return err
		}

		var session *storage.Session
		if resume != "" {
			session, err = api.Status(resume)
		} else {
			session, err = api.Init(*request)
		}
		if err != nil {
			return err
		}

		remaining := len(session.Remaining())
		if remaining < session.TotalChunks {
			output.Note("resuming %s: %d of %d chunks already uploaded",
				session.UploadID, session.TotalChunks-remaining, session.TotalChunks)
		}

		var progress storage.Progress
		if quiet, _ := flags.GetBool("quiet"); !quiet {
			progress = func(current *storage.Session, index int) {
				output.Note("chunk %d done (%d/%d)", index,
					len(current.UploadedChunks), current.TotalChunks)
			}
		}

		final, err := api.SendFile(session, args[0], progress)
		if err != nil {
			if final != nil {
				output.Note("resume with `--resume %s`", final.UploadID)
			}
			return err
		}
		return output.Success(final, nil, nil)
	},
}

var fsSessionInitCmd = &cobra.Command{
	Use:   "init <file>",
	Short: "Open a session without sending anything",
	Long: `Reports the chunk size and count the server chose, which is what you need to
drive the transfer by hand.`,
	Args: exactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		request, err := initRequest(cmd, args[0])
		if err != nil {
			return err
		}
		api, err := storageAPI()
		if err != nil {
			return err
		}
		session, err := api.Init(*request)
		if err != nil {
			return err
		}
		return output.Success(session, nil, nil)
	},
}

// initRequest reads the file's real size, because the server derives the chunk
// count from it and a wrong figure makes `complete` demand chunks that will
// never exist.
func initRequest(cmd *cobra.Command, filePath string) (*storage.InitRequest, error) {
	flags := cmd.Flags()
	targetType, _ := flags.GetString("target-type")
	targetID, _ := flags.GetString("target-id")
	if targetType == "" || targetID == "" {
		return nil, cliexit.Usage("--target-type and --target-id are both required")
	}
	if err := requireChoice("--target-type", targetType, storage.TargetTypes); err != nil {
		return nil, err
	}

	info, err := os.Stat(filePath)
	if err != nil {
		return nil, cliexit.Usage("cannot read %q: %v", filePath, err)
	}
	if info.IsDir() {
		return nil, cliexit.Usage("%q is a directory", filePath)
	}

	request := &storage.InitRequest{
		TargetType: targetType,
		TargetID:   targetID,
		FileName:   filepath.Base(filePath),
		FileSize:   info.Size(),
		MimeType:   mime.TypeByExtension(filepath.Ext(filePath)),
	}
	if path, _ := flags.GetString("target-path"); path != "" {
		request.TargetPath = path
	}
	if name, _ := flags.GetString("name"); name != "" {
		request.FileName = name
	}
	if checksum, _ := flags.GetString("checksum"); checksum != "" {
		request.Checksum = checksum
	}
	if action, _ := flags.GetString("post-action"); action != "" {
		if err := requireChoice("--post-action", action, storage.PostActions); err != nil {
			return nil, err
		}
		request.PostAction = action
	}
	if policy, _ := flags.GetString("on-conflict"); policy != "" {
		if err := requireChoice("--on-conflict", policy, storage.ConflictPolicies); err != nil {
			return nil, err
		}
		request.ConflictPolicy = policy
	}
	if size, _ := flags.GetInt("chunk-size"); size > 0 {
		if size < 1<<20 || size > 128<<20 {
			return nil, cliexit.Usage("--chunk-size must be between 1048576 and 134217728 bytes, received %d", size)
		}
		request.ChunkSize = size
	}
	return request, nil
}

var fsSessionStatusCmd = &cobra.Command{
	Use:   "status <upload-id>",
	Short: "Show a session's progress",
	Args:  exactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		api, err := storageAPI()
		if err != nil {
			return err
		}
		session, err := api.Status(args[0])
		if err != nil {
			return err
		}
		return output.Success(session, []string{"FIELD", "VALUE"}, func() [][]string {
			return [][]string{
				{"uploadId", session.UploadID},
				{"fileName", session.FileName},
				{"status", session.Status},
				{"chunks", strconv.Itoa(len(session.UploadedChunks)) + "/" + strconv.Itoa(session.TotalChunks)},
				{"chunkSize", strconv.Itoa(session.ChunkSize)},
				{"postAction", session.PostAction},
				{"expiresAt", session.ExpiresAt},
			}
		})
	},
}

var fsSessionChunkCmd = &cobra.Command{
	Use:   "chunk <upload-id> <index> <file>",
	Short: "Send one chunk, read from the file at that offset",
	Long: `The offset is index × chunkSize, taken from the session, so the same file can
be used for every chunk.

  infini-cli fs session chunk <uploadId> 0 ./dump.csv`,
	Args: exactArgs(3),
	RunE: func(cmd *cobra.Command, args []string) error {
		index, err := strconv.Atoi(args[1])
		if err != nil || index < 0 {
			return cliexit.Usage("the chunk index must be a non-negative integer, received %q", args[1])
		}

		api, err := storageAPI()
		if err != nil {
			return err
		}
		session, err := api.Status(args[0])
		if err != nil {
			return err
		}
		if index >= session.TotalChunks {
			return cliexit.Usage("this session has %d chunk(s), so %d is out of range",
				session.TotalChunks, index)
		}

		file, err := os.Open(args[2])
		if err != nil {
			return cliexit.Usage("cannot read %q: %v", args[2], err)
		}
		defer file.Close()

		info, err := file.Stat()
		if err != nil {
			return cliexit.Usage("cannot inspect %q: %v", args[2], err)
		}
		offset := int64(index) * int64(session.ChunkSize)
		length := min(int64(session.ChunkSize), info.Size()-offset)
		if length <= 0 {
			return cliexit.Usage("chunk %d starts past the end of %q", index, args[2])
		}

		updated, err := api.SendChunk(args[0], index, io.NewSectionReader(file, offset, length), length)
		if err != nil {
			return err
		}
		return output.Success(updated, nil, nil)
	},
}

var fsSessionCompleteCmd = &cobra.Command{
	Use:   "complete <upload-id>",
	Short: "Assemble the chunks and run the post-action",
	Long: `Fails rather than assembling a partial file when a chunk is missing;
` + "`fs session status`" + ` shows which ones arrived.`,
	Args: exactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		api, err := storageAPI()
		if err != nil {
			return err
		}
		session, err := api.Complete(args[0])
		if err != nil {
			return err
		}
		return output.Success(session, nil, nil)
	},
}

var fsSessionCancelCmd = &cobra.Command{
	Use:   "cancel <upload-id>",
	Short: "Abandon a session and discard its chunks",
	Args:  exactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := confirm("Cancel upload " + args[0] + " and discard its chunks?"); err != nil {
			return err
		}
		api, err := storageAPI()
		if err != nil {
			return err
		}
		raw, err := api.Cancel(args[0])
		if err != nil {
			return err
		}
		return output.Success(normalizeRaw(raw), nil, nil)
	},
}

func addSessionInitFlags(cmd *cobra.Command) {
	flags := cmd.Flags()
	flags.String("target-type", "", "Where it lands: "+strings.Join(storage.TargetTypes, ", "))
	flags.String("target-id", "", "Id of the target resource")
	flags.String("target-path", "", "Relative directory inside the target, e.g. data/raw")
	flags.String("name", "", "Store it under a different name")
	flags.String("checksum", "", "Checksum of the whole file")
	flags.String("post-action", "", "What to do once assembled: "+strings.Join(storage.PostActions, ", "))
	flags.String("on-conflict", "", "If the name is taken: "+strings.Join(storage.ConflictPolicies, ", "))
	flags.Int("chunk-size", 0, "Chunk size in bytes (1 MiB – 128 MiB)")
}

func init() {
	addSessionInitFlags(fsSessionPushCmd)
	fsSessionPushCmd.Flags().String("resume", "", "Continue this session instead of opening a new one")
	fsSessionPushCmd.Flags().Bool("quiet", false, "Do not report per-chunk progress")
	addSessionInitFlags(fsSessionInitCmd)

	fsSessionCmd.AddCommand(
		fsSessionPushCmd, fsSessionInitCmd, fsSessionStatusCmd,
		fsSessionChunkCmd, fsSessionCompleteCmd, fsSessionCancelCmd,
	)
	fsCmd.AddCommand(fsSessionCmd)
}
