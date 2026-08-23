# Twin Manager - Documentation

A minimalistic two-pane TUI file manager in Go with Norton-style commands and "starts with" active search.

## Tech Stack

*   **Go:** The programming language used for the project.
*   **Bubble Tea:** A TUI framework for building terminal applications.
*   **Lipgloss:** A library for styling terminal output.

## Features

*   **Two-pane layout:** A classic two-pane file manager interface.
*   **File navigation:** Navigate through the file system using the arrow keys, `home`, `end`, `pgup`, and `pgdown`.
*   **Parent Navigation:** Navigate to the parent directory by selecting the `..` entry.
*   **File selection:** Select multiple files using `Insert`.
*   **File operations:**
    *   **Copy (Alt+C / F5):** Copy selected files from the active pane to the inactive pane.
    *   **Move (Alt+M / F6):** Move selected files from the active pane to the inactive pane.
    *   **Delete (Alt+D / F8):** Delete the selected file or folder.
    *   **New Folder (Alt+N / F7):** Create a new folder in the active pane.
    *   **Copy Path (Alt+P / F9):** Copy the full path of selected files to the system clipboard.
    *   **Preview (Alt+V / F3):** Preview the selected file.
    *   **Jobs (Alt+J):** Open the operation queue panel.
    *   **Quit (Alt+Q / F10):** Quit the application.
    *   **Force Quit (Ctrl+C):** Force quit the application.
*   **Overwrite confirmation:** A confirmation prompt is displayed when a file operation would overwrite an existing file. Answer per file with `y`/`n`, or `A` to overwrite everything remaining and `s` to skip everything remaining. `Esc` abandons the whole operation. Files that do not conflict are copied regardless of how you answer.
*   **Operation queue:** Copy, move, and delete run in the background, one at a time, in the order they were started. The UI stays usable while they run.
    *   **Progress indicator:** A compact widget in the bottom-right corner shows the running operation, a progress bar, transferred/total size, current speed, and an ETA. It shares the hints row, so it takes no extra vertical space, and disappears a few seconds after the last operation finishes.
    *   **Queue panel (Alt+J):** Lists queued, running, finished, and failed operations. `c` cancels the highlighted operation (whether or not it has started), `x` dismisses a finished entry, `Esc` closes the panel.
    *   **Cancellation:** Cancelling stops the transfer at the next chunk boundary. A cancelled move never removes its source, because the source is only unlinked after the copy has landed.
*   **Active search:** Start typing to search for files in the active pane.
*   **File preview:** Preview the content of the selected file in a full-screen overlay.
    *   **Scrollable:** Use `up`, `down`, `pgup`, `pgdown`, `home`, and `end` to scroll through the preview content.

## Technical Details

### Model-View-Update (MVU) Architecture

The application follows the Model-View-Update (MVU) architecture provided by the Bubble Tea framework.

*   **Model (`model` struct):** The `model` struct holds the entire state of the application, including the state of the two panes, the current operation (e.g., creating a folder, deleting a file), preview state, and any error messages.
*   **View (`View` function):** The `View` function is responsible for rendering the UI based on the current state of the model. It uses the `lipgloss` library for styling.
*   **Update (`Update` function):** The `Update` function handles all incoming messages (e.g., key presses, window resizing) and updates the model accordingly.

### Panes

The two panes are represented by the `pane` struct, which holds the state of a single pane, including the current path, the list of files, the cursor position, and the selected files.

### File Operations

Copy, move, and delete go through the operation queue in `queue.go`. Pressing the shortcut does not start any I/O: it runs `probeConflictsCmd`, which only checks the destination and splits the sources into "no conflict" and "needs an answer". Once the overwrite prompt is resolved, the approved files are appended to the queue as a single `fileOp`.

`Update` dispatches one `fileOp` at a time via `runOpCmd`. Because Bubble Tea runs every command on its own goroutine, the worker can block for the whole transfer; the UI stays live because progress is published into atomic counters on the `fileOp` and read back by a 100 ms `progressTickMsg`, rather than being pushed through a channel. Completion arrives as `opFinishedMsg`, which reloads both panes and starts the next queued operation.

Each operation carries a `context.Context`, so cancelling from the queue panel stops the copy loop at the next chunk. A cross-filesystem move (where `os.Rename` returns `EXDEV`) falls back to copy-then-delete, and only unlinks the source once the copy has succeeded.

### Preview

The file preview feature is implemented by setting a `isPreviewing` flag in the model. When this flag is true, the `View` function renders the preview content in an overlay instead of the two panes. The file content is read by the `previewFileCmd` command. The preview supports scrolling by tracking a `previewScrollY` offset in the model.

### Layout Improvements

Recent updates have addressed layout issues to ensure consistent pane heights and correct rendering within the terminal window, preventing rows from being cut off or panes from overflowing.
