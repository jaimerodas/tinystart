package web

import (
	"context"
	"strconv"

	"github.com/jaimerodas/tinystart/internal/store"
)

// This file is StartPageHelper: the dom ids every stream aims at, the keyboard
// shortcuts both pages list, and the small structs the start page templates
// are handed. It holds no HTTP and no SQL — handle_start.go and handle_editor.go
// bring those — so that "what does the editor draw" can be read in one place.

// The Turbo Stream targets. Every write on the editor replaces the smallest
// node that can have changed, so these ids are named by the handlers, the
// templates and the tests alike; they live here so they exist only once.
func columnDOMID(column int) string { return "column_" + strconv.Itoa(column) }

func newGroupDOMID(column int) string { return "new_group_column_" + strconv.Itoa(column) }

func groupDOMID(id int64) string { return "group_" + strconv.FormatInt(id, 10) }

func itemDOMID(id int64) string { return "item_" + strconv.FormatInt(id, 10) }

func newItemDOMID(groupID int64) string { return "new_item_group_" + strconv.FormatInt(groupID, 10) }

// Not a stream target: this one names the group's <section> through
// aria-labelledby, which is what makes it a region at all.
func groupNameDOMID(id int64) string { return "group_name_" + strconv.FormatInt(id, 10) }

const (
	// columnCountDOMID is the toolbar's column picker. A refused change
	// replaces it, because it is left showing the value the database would
	// not take.
	columnCountDOMID = "column_count"

	// noticeDOMID is the live region a refused move speaks through. It is
	// updated and never replaced — see writeNotice.
	noticeDOMID = "start_page_notice"
)

// shortcut is one row of the ? dialog: the keys to show, and what they do.
type shortcut struct {
	Keys        []string
	Description string
}

// The shortcuts as data rather than as markup, because two views render them
// and the editor's toolbar legend points at the dialog rather than repeating
// it. Keeping them here is what stops the dialog drifting from the keys
// grid_keyboard_controller.js and start_shortcuts_controller.js implement.
//
// The page chords are matched on event.code in start_shortcuts_controller, so
// they are ⌥E and ⌥S whatever the keyboard layout puts under those keys.
var gridShortcuts = []shortcut{
	{[]string{"↑", "↓", "←", "→"}, "move between tiles"},
	{[]string{"Home", "End"}, "first / last in the column"},
	{[]string{"Space"}, "pick up / drop"},
	{[]string{"Enter"}, "edit"},
	{[]string{"Del", "⌫"}, "delete"},
	{[]string{"Esc"}, "cancel a move"},
	{[]string{"Tab"}, "leave the grid"},
}

var showPageShortcuts = []shortcut{
	{[]string{"⌥", "E"}, "edit the start page"},
	{[]string{"?"}, "show this list"},
	{[]string{"↑", "↓"}, "move through results"},
	{[]string{"Enter"}, "open"},
	{[]string{"⌘", "Enter"}, "open in a new tab"},
	{[]string{"Esc"}, "clear the bar, then leave it"},
}

// editorShortcuts is the grid's keys plus the two page chords. The grid's keys
// are only written down once, above: the legend stopped listing them when it
// became a pointer to this dialog.
var editorShortcuts = append([]shortcut{
	{[]string{"⌥", "S"}, "back to the start page"},
	{[]string{"?"}, "show this list"},
}, gridShortcuts...)

// The three states the command bar's "All Links" section can be in. Nothing to
// search means no section at all; a rejected token means a notice rather than
// a query that will only be rejected again.
const (
	federationOff       = "off"
	federationReconnect = "reconnect"
	federationActive    = "active"
)

func federationState(connection *store.Connection) string {
	switch {
	case connection == nil:
		return federationOff
	case connection.NeedsReconnect():
		return federationReconnect
	default:
		return federationActive
	}
}

// --- the editor's view types ---
//
// One struct per partial, holding what that partial draws and nothing else.
// They are built by the handlers and passed straight through, so a template
// never has to reach back into the store or ask a question the handler could
// have answered.

// columnView is one column of the grid: the groups in it and the add-group
// slot at its foot.
type columnView struct {
	Number   int
	Groups   []groupView
	NewGroup newGroupView
}

func (c columnView) DOMID() string { return columnDOMID(c.Number) }

// groupView is one group as the editor draws it: the stored group, its tiles,
// the rename form behind the pencil, and the add-link slot at its foot.
type groupView struct {
	ID     int64
	Name   string // saved_name: what the header shows, even mid-refusal
	Column int
	Items  []itemView
	// FormOpen is the :edit_group half of the controllers' open_form. A
	// rejected rename comes back with the form showing, so the errors stay
	// with the values that caused them.
	FormOpen bool
	Form     groupForm
	NewItem  newItemView
}

func (g groupView) DOMID() string     { return groupDOMID(g.ID) }
func (g groupView) NameDOMID() string { return groupNameDOMID(g.ID) }

// itemView is one tile: the row, and the edit form that is already beside it
// so the pencil opens without a round trip.
type itemView struct {
	ID       int64
	GroupID  int64
	Title    string // saved_title, for the same reason groupView.Name is saved_name
	Position int
	FormOpen bool
	Form     itemForm
}

func (i itemView) DOMID() string { return itemDOMID(i.ID) }

// newGroupView and newItemView are the add-group and add-link slots. Open is
// what the handlers set: true after a rejected create, so the errors arrive
// with the form still showing, and true after a successful add of a tile, so
// the next link can be typed straight away.
type newGroupView struct {
	Column int
	Open   bool
	Form   groupForm
}

func (n newGroupView) DOMID() string { return newGroupDOMID(n.Column) }

type newItemView struct {
	GroupID   int64
	GroupName string // saved_name, for the trigger's accessible name
	Open      bool
	Form      itemForm
}

func (n newItemView) DOMID() string { return newItemDOMID(n.GroupID) }

// groupForm is the state of the one form that both adds a group and renames
// one: what is in the field, what Cancel restores, and the messages above it.
//
// Name and Pristine part company only after a refusal. The field has to show
// what was typed or the correction is lost, and Cancel has to restore what is
// on disk or "discard" would mean "keep the value the database refused".
type groupForm struct {
	ID       int64 // zero while the group does not exist yet
	Name     string
	Pristine string
	Column   int // rendered as a hidden field only while adding
	// Typed says the fields hold what someone put in them rather than what is
	// on disk. See ShowValue.
	Typed     bool
	Errors    []string
	NameError bool
}

func (f groupForm) Persisted() bool { return f.ID != 0 }

// ShowValue is Rails' rule for the value attribute: it is written whenever the
// attribute is not nil, and the attribute is nil only on a record nobody has
// filled in yet. So a fresh add form has no value attribute at all, while a
// rejected save renders value="" — otherwise a field someone cleared would
// come back holding the old text.
func (f groupForm) ShowValue() bool { return f.Persisted() || f.Typed }

func (f groupForm) Action() string {
	if f.Persisted() {
		return "/start/groups/" + strconv.FormatInt(f.ID, 10)
	}
	return "/start/groups"
}

func (f groupForm) Submit() string {
	if f.Persisted() {
		return "Save"
	}
	return "Add"
}

// itemForm is the same for a tile, which has two fields to keep track of.
type itemForm struct {
	ID            int64 // zero while the tile does not exist yet
	GroupID       int64
	Title         string
	URL           string
	PristineTitle string
	PristineURL   string
	Typed         bool
	Errors        []string
	TitleError    bool
	URLError      bool
}

func (f itemForm) Persisted() bool { return f.ID != 0 }

// ShowValue is groupForm.ShowValue, and for the same reason: both fields are
// filled in by the same submission, so one flag answers for the pair.
func (f itemForm) ShowValue() bool { return f.Persisted() || f.Typed }

func (f itemForm) Action() string {
	if f.Persisted() {
		return "/start/items/" + strconv.FormatInt(f.ID, 10)
	}
	return "/start/items"
}

func (f itemForm) Submit() string {
	if f.Persisted() {
		return "Save"
	}
	return "Add"
}

// --- building the views ---

// columnViewFor gathers everything the column partial draws. It is what the
// group handlers answer a create, a destroy or a move with, and what the
// editor page calls once per column.
func (s *Server) columnViewFor(ctx context.Context, userID int64, column int) (columnView, error) {
	groups, err := s.db.GroupsInColumn(ctx, userID, column)
	if err != nil {
		return columnView{}, err
	}
	return s.columnViewFrom(ctx, column, groups)
}

func (s *Server) columnViewFrom(ctx context.Context, column int, groups []store.Group) (columnView, error) {
	view := columnView{
		Number:   column,
		NewGroup: newGroupView{Column: column, Form: groupForm{Column: column}},
	}
	for _, group := range groups {
		groupView, err := s.groupViewFor(ctx, group)
		if err != nil {
			return columnView{}, err
		}
		view.Groups = append(view.Groups, groupView)
	}
	return view, nil
}

// groupViewFor is one group with its tiles, with both forms closed — the shape
// every read of the page produces. The handlers that need a form open take
// this and set the one flag.
func (s *Server) groupViewFor(ctx context.Context, group store.Group) (groupView, error) {
	items, err := s.db.ItemsInGroup(ctx, group.ID)
	if err != nil {
		return groupView{}, err
	}

	view := groupView{
		ID:     group.ID,
		Name:   group.Name,
		Column: group.Column,
		Form:   groupForm{ID: group.ID, Name: group.Name, Pristine: group.Name},
		NewItem: newItemView{
			GroupID:   group.ID,
			GroupName: group.Name,
			Form:      itemForm{GroupID: group.ID},
		},
	}
	for _, item := range items {
		view.Items = append(view.Items, itemViewFor(item))
	}
	return view, nil
}

func itemViewFor(item store.Item) itemView {
	return itemView{
		ID:       item.ID,
		GroupID:  item.GroupID,
		Title:    item.Title,
		Position: item.Position,
		Form: itemForm{
			ID:            item.ID,
			GroupID:       item.GroupID,
			Title:         item.Title,
			URL:           item.URL,
			PristineTitle: item.Title,
			PristineURL:   item.URL,
		},
	}
}
