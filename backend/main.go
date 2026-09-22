package main

import (
	"encoding/json"
	"errors"
	"log"
	"sync"

	dbxpluginsdk "github.com/t8y2/dbx/plugins/sdk/go/dbx-plugin-sdk"
)

type plugin struct {
	mutex       sync.RWMutex
	connections map[string]*connNotes
	notes       *notesStore
	exports     *exportStore
}

// workbenchNamespace is the storage namespace used when the host addresses the
// workbench directly instead of through a connection. It is a fixed string, so
// notes survive the transition and never depend on a host-assigned id.
const workbenchNamespace = "workbench"

func (plugin *plugin) Handle(
	_ dbxpluginsdk.RequestContext,
	method string,
	params json.RawMessage,
	_ *dbxpluginsdk.Emitter,
) (any, *dbxpluginsdk.PluginError) {
	var values map[string]any
	if err := json.Unmarshal(params, &values); err != nil {
		return nil, dbxpluginsdk.NewError(-32602, "Invalid request parameters")
	}
	switch method {
	case "connection/test":
		connection, _ := values["connection"].(map[string]any)
		return map[string]any{
			"success":     true,
			"message":     "DBX Calendar is ready",
			"connection":  connection,
			"persistence": plugin.notes.Persistence(),
		}, nil
	case "connection/connect":
		connectionID, pluginError := requestConnectionID(values)
		if pluginError != nil {
			return nil, pluginError
		}
		if _, pluginError := plugin.openConnection(connectionID); pluginError != nil {
			return nil, pluginError
		}
		return map[string]any{"success": true, "persistence": plugin.notes.Persistence()}, nil
	case "connection/disconnect":
		connectionID, pluginError := requestConnectionID(values)
		if pluginError != nil {
			return nil, pluginError
		}
		plugin.mutex.Lock()
		delete(plugin.connections, connectionID)
		plugin.mutex.Unlock()
		return map[string]any{"success": true}, nil
	case "dbx-calendar/ping":
		return map[string]any{"ok": true, "plugin": "com.tenltrs.dbx-calendar", "language": "go", "connectionId": values["connectionId"]}, nil
	case "dbx-calendar/notes/list":
		notes, pluginError := plugin.notesFor(values)
		if pluginError != nil {
			return nil, pluginError
		}
		all, err := notes.Notes()
		if err != nil {
			return nil, dbxpluginsdk.NewError(-32603, err.Error())
		}
		return map[string]any{"notes": all, "persistence": plugin.notes.Persistence()}, nil
	case "dbx-calendar/notes/get":
		notes, pluginError := plugin.notesFor(values)
		if pluginError != nil {
			return nil, pluginError
		}
		dateKey, _ := values["date"].(string)
		value, ok, err := notes.Note(dateKey)
		if err != nil {
			return nil, dbxpluginsdk.NewError(-32603, err.Error())
		}
		return map[string]any{"date": dateKey, "value": value, "exists": ok}, nil
	case "dbx-calendar/notes/set":
		notes, pluginError := plugin.notesFor(values)
		if pluginError != nil {
			return nil, pluginError
		}
		dateKey, _ := values["date"].(string)
		value, _ := values["value"].(string)
		if err := notes.Set(dateKey, value); err != nil {
			return nil, notesError(err)
		}
		all, err := notes.Notes()
		if err != nil {
			return nil, dbxpluginsdk.NewError(-32603, err.Error())
		}
		return map[string]any{"success": true, "date": dateKey, "notes": all}, nil
	case "dbx-calendar/notes/replace":
		notes, pluginError := plugin.notesFor(values)
		if pluginError != nil {
			return nil, pluginError
		}
		incoming, ok := decodeNoteMap(values["notes"])
		if !ok {
			return nil, dbxpluginsdk.NewError(-32602, "Notes must be an object of date to text")
		}
		if err := notes.Replace(incoming); err != nil {
			return nil, notesError(err)
		}
		all, err := notes.Notes()
		if err != nil {
			return nil, dbxpluginsdk.NewError(-32603, err.Error())
		}
		return map[string]any{"success": true, "notes": all}, nil
	case "dbx-calendar/export/destination":
		return map[string]any{"directory": plugin.exports.Destination()}, nil
	case "dbx-calendar/export/pick":
		initial, title, label, extension, err := decodePick(values)
		if err != nil {
			return nil, dbxpluginsdk.NewError(-32602, err.Error())
		}
		chosen, pickErr := pickSavePath(initial, title, label, extension)
		if pickErr != nil {
			return nil, dbxpluginsdk.NewError(-32603, pickErr.Error())
		}
		if chosen == "" {
			return map[string]any{"cancelled": true}, nil
		}
		plugin.exports.Remember(chosen)
		return map[string]any{"path": chosen, "cancelled": false}, nil
	case "dbx-calendar/export/write":
		path, encoded, overwrite, err := decodeExport(values)
		if err != nil {
			return nil, dbxpluginsdk.NewError(-32602, err.Error())
		}
		written, writeErr := plugin.exports.Write(path, encoded, overwrite)
		if writeErr != nil {
			// An oversized payload is the caller's mistake; anything else is
			// this side failing to reach the disk it was pointed at.
			if errors.Is(writeErr, ErrExportTooLarge) {
				return nil, dbxpluginsdk.NewError(-32602, writeErr.Error())
			}
			return nil, dbxpluginsdk.NewError(-32603, writeErr.Error())
		}
		plugin.exports.Remember(written.Path)
		return map[string]any{
			"success":   true,
			"path":      written.Path,
			"size":      written.Size,
			"directory": written.Directory,
		}, nil
	case "dbx-calendar/export/reveal":
		path, _ := values["path"].(string)
		folder, revealErr := revealInExplorer(path)
		if revealErr != nil {
			return nil, dbxpluginsdk.NewError(-32603, revealErr.Error())
		}
		return map[string]any{"success": true, "folder": folder}, nil
	case "dbx-calendar/export/copy":
		text, _ := values["text"].(string)
		if text == "" {
			return nil, dbxpluginsdk.NewError(-32602, "Missing required parameter: text")
		}
		if copyErr := setClipboard(text); copyErr != nil {
			return nil, dbxpluginsdk.NewError(-32603, copyErr.Error())
		}
		return map[string]any{"success": true}, nil
	default:
		return nil, dbxpluginsdk.MethodNotFound(method)
	}
}

// notesFor resolves the per-connection handle. The plugin lock is released
// before any file IO, so one slow connection cannot block the others. A pure
// workbench plugin is addressed without a connection, which falls back to the
// fixed workbench namespace.
func (plugin *plugin) notesFor(values map[string]any) (*connNotes, *dbxpluginsdk.PluginError) {
	connectionID := optionalConnectionID(values)
	plugin.mutex.RLock()
	notes, ok := plugin.connections[connectionID]
	plugin.mutex.RUnlock()
	if ok {
		return notes, nil
	}
	// The host may address a connection, or the workbench, for the first time.
	return plugin.openConnection(connectionID)
}

func (plugin *plugin) openConnection(connectionID string) (*connNotes, *dbxpluginsdk.PluginError) {
	notes, err := plugin.notes.Open(connectionID)
	if err != nil {
		return nil, notesError(err)
	}
	plugin.mutex.Lock()
	plugin.connections[connectionID] = notes
	plugin.mutex.Unlock()
	return notes, nil
}

// notesError maps a store failure onto the protocol. A missing data directory
// is a host configuration problem, not an internal error, and the UI must see
// it rather than believe a note was saved.
func notesError(err error) *dbxpluginsdk.PluginError {
	if errors.Is(err, ErrNoDataDir) {
		return dbxpluginsdk.NewError(-32002, "Notes cannot be persisted: plugin data directory is unavailable")
	}
	return dbxpluginsdk.NewError(-32602, err.Error())
}

func requestConnectionID(values map[string]any) (string, *dbxpluginsdk.PluginError) {
	connectionID := optionalConnectionID(values)
	if connectionID == "" {
		return "", dbxpluginsdk.NewError(-32602, "Missing connection id")
	}
	return connectionID, nil
}

// decodeNoteMap reads the whole note set an import sends. Keys are validated by
// the store, so this only has to reject a payload of the wrong shape.
func decodeNoteMap(value any) (map[string]string, bool) {
	raw, ok := value.(map[string]any)
	if !ok {
		return nil, false
	}
	notes := make(map[string]string, len(raw))
	for key, entry := range raw {
		text, ok := entry.(string)
		if !ok {
			return nil, false
		}
		notes[key] = text
	}
	return notes, true
}

// optionalConnectionID reads the connection the request targets, or "" when the
// caller is the workbench itself.
func optionalConnectionID(values map[string]any) string {
	connection, _ := values["connection"].(map[string]any)
	connectionID, _ := connection["id"].(string)
	if connectionID != "" {
		return connectionID
	}
	if direct, _ := values["connectionId"].(string); direct != "" {
		return direct
	}
	return workbenchNamespace
}

func main() {
	// Resolve the host-provided data directory once. EnsureDataDir also creates
	// it, so the first note write does not have to mkdir first. Running outside
	// a DBX host (tests, standalone debugging) leaves this empty, which keeps
	// the sidecar functional but non-persistent.
	dataDir, err := ensureDataDir()
	if err != nil {
		log.Printf("dbx-calendar: persistent notes disabled: %v", err)
	}

	metadata := dbxpluginsdk.Metadata{
		ID:           "com.tenltrs.dbx-calendar",
		Version:      "0.1.5",
		Capabilities: []string{"commands"},
	}
	server := dbxpluginsdk.NewServer(metadata, &plugin{
		connections: map[string]*connNotes{},
		notes:       newNotesStore(dataDir),
		exports:     newExportStore(dataDir),
	})
	if err := server.Serve(); err != nil {
		log.Fatal(err)
	}
}
