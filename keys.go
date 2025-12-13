package main

// Shortcut represents a keyboard shortcut with a description.
type Shortcut struct {
	Key        string // The actual key to press (e.g., "alt+c", "q")
	DisplayKey string // The key to show in the UI (e.g., "c")
	FKey       string // Function key alias (e.g., "f5")
	Modifier   string // "ctrl", "alt", "shift", or empty
	Action     string // Description for the UI
	Cmd        string // Internal command identifier
}

// KeyMap holds all application shortcuts.
type KeyMap struct {
	Quit            Shortcut
	ForceQuit       Shortcut
	SwitchPane      Shortcut
	Preview         Shortcut
	Copy            Shortcut
	Move            Shortcut
	NewFolder       Shortcut
	Delete          Shortcut
	CopyPath        Shortcut
	ToggleSelection Shortcut
}

// DefaultKeyMap returns the default key mapping.
func DefaultKeyMap() KeyMap {
	return KeyMap{
		Quit:            Shortcut{Key: "alt+q", DisplayKey: "q", FKey: "f10", Modifier: "alt", Action: "Quit", Cmd: "quit"},
		ForceQuit:       Shortcut{Key: "ctrl+c", DisplayKey: "c", Modifier: "ctrl", Action: "Force Quit", Cmd: "force_quit"},
		SwitchPane:      Shortcut{Key: "tab", DisplayKey: "tab", Modifier: "", Action: "Switch Pane", Cmd: "switch_pane"},
		Preview:         Shortcut{Key: "alt+v", DisplayKey: "v", FKey: "f3", Modifier: "alt", Action: "View", Cmd: "preview"},
		Copy:            Shortcut{Key: "alt+c", DisplayKey: "c", FKey: "f5", Modifier: "alt", Action: "Copy", Cmd: "copy"},
		Move:            Shortcut{Key: "alt+m", DisplayKey: "m", FKey: "f6", Modifier: "alt", Action: "Move", Cmd: "move"},
		NewFolder:       Shortcut{Key: "alt+n", DisplayKey: "n", FKey: "f7", Modifier: "alt", Action: "MkDir", Cmd: "mkdir"},
		Delete:          Shortcut{Key: "alt+d", DisplayKey: "d", FKey: "f8", Modifier: "alt", Action: "Delete", Cmd: "delete"},
		CopyPath:        Shortcut{Key: "alt+p", DisplayKey: "p", FKey: "f9", Modifier: "alt", Action: "Copy Path", Cmd: "copy_path"},
		ToggleSelection: Shortcut{Key: "alt+i", DisplayKey: "i", Modifier: "alt", Action: "Select", Cmd: "select"},
	}
}

// GetShortcuts returns a slice of all shortcuts for iteration.
func (k KeyMap) GetShortcuts() []Shortcut {
	return []Shortcut{
		k.Quit,
		k.ForceQuit,
		k.SwitchPane,
		k.Preview,
		k.Copy,
		k.Move,
		k.NewFolder,
		k.Delete,
		k.CopyPath,
		k.ToggleSelection,
	}
}

// GetAliasMap returns a map of aliases to their primary keys
func (k KeyMap) GetAliasMap() map[string]string {
	aliases := make(map[string]string)
	for _, s := range k.GetShortcuts() {
		if s.FKey != "" {
			aliases[s.FKey] = s.Key
		}
	}
	return aliases
}
