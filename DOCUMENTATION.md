# Double Manager - Documentation

A minimalistic two-pane TUI file manager in Go with Norton-style commands and "starts with" active search.

## Tech Stack

*   **Go:** The programming language used for the project.
*   **Bubble Tea:** A TUI framework for building terminal applications.
*   **Lipgloss:** A library for styling terminal output.

## Features

*   **Two-pane layout:** A classic two-pane file manager interface.
*   **File navigation:** Navigate through the file system using the arrow keys, `home`, `end`, `pgup`, and `pgdown`.
*   **Parent Navigation:** Navigate to the parent directory by selecting the `..` entry.
*   **File selection:** Select multiple files using `Alt+I` or `Control+I`.
*   **File operations:**
    *   **Copy (Alt+C / F5):** Copy selected files from the active pane to the inactive pane.
    *   **Move (Alt+M / F6):** Move selected files from the active pane to the inactive pane.
    *   **Delete (Alt+D / F8):** Delete the selected file or folder.
    *   **New Folder (Alt+N / F7):** Create a new folder in the active pane.
    *   **Copy Path (Alt+P / F9):** Copy the full path of selected files to the system clipboard.
    *   **Preview (Alt+V / F3):** Preview the selected file.
    *   **Quit (Alt+Q / F10):** Quit the application.
    *   **Force Quit (Ctrl+C):** Force quit the application.
*   **Overwrite confirmation:** A confirmation prompt is displayed when a file operation would overwrite an existing file.
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

File operations are handled by sending commands (e.g., `copyFilesCmd`, `moveFilesCmd`, `deleteFileCmd`) from the `Update` function. These commands are functions that perform the file system operations and return a message to the `Update` function to signal completion or an error.

### Preview

The file preview feature is implemented by setting a `isPreviewing` flag in the model. When this flag is true, the `View` function renders the preview content in an overlay instead of the two panes. The file content is read by the `previewFileCmd` command. The preview supports scrolling by tracking a `previewScrollY` offset in the model.

### Layout Improvements

Recent updates have addressed layout issues to ensure consistent pane heights and correct rendering within the terminal window, preventing rows from being cut off or panes from overflowing.
