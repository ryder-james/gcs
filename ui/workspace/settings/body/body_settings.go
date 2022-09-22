package body

import (
	"fmt"
	"io/fs"

	"github.com/richardwilkes/gcs/v5/model/gurps"
	"github.com/richardwilkes/gcs/v5/model/library"
	"github.com/richardwilkes/gcs/v5/model/settings"
	"github.com/richardwilkes/gcs/v5/res"
	"github.com/richardwilkes/gcs/v5/ui/widget"
	"github.com/richardwilkes/gcs/v5/ui/workspace"
	wsettings "github.com/richardwilkes/gcs/v5/ui/workspace/settings"
	"github.com/richardwilkes/toolbox/i18n"
	"github.com/richardwilkes/toolbox/log/jot"
	"github.com/richardwilkes/unison"
	"golang.org/x/exp/slices"
)

var _ widget.GroupedCloser = &bodyDockable{}

type bodyDockable struct {
	wsettings.Dockable
	owner          widget.EntityPanel
	targetMgr      *widget.TargetMgr
	undoMgr        *unison.UndoManager
	body           *gurps.Body
	originalCRC    uint64
	toolbar        *unison.Panel
	content        *unison.Panel
	applyButton    *unison.Button
	cancelButton   *unison.Button
	dragTarget     *unison.Panel
	dragTargetBody *gurps.Body
	dragInsert     int
	promptForSave  bool
	inDragOver     bool
}

// ShowBodySettings the Body Settings. Pass in nil to edit the defaults or a sheet to edit the sheet's.
func ShowBodySettings(owner widget.EntityPanel) {
	ws, dc, found := workspace.Activate(func(d unison.Dockable) bool {
		if s, ok := d.(*bodyDockable); ok && owner == s.owner {
			return true
		}
		return false
	})
	if !found && ws != nil {
		d := &bodyDockable{
			owner:         owner,
			promptForSave: true,
		}
		d.Self = d
		d.targetMgr = widget.NewTargetMgr(d)
		if owner != nil {
			entity := d.owner.Entity()
			d.body = entity.SheetSettings.BodyType.Clone(entity, nil)
			d.TabTitle = i18n.Text("Body Type: " + owner.Entity().Profile.Name)
		} else {
			d.body = settings.Global().Sheet.BodyType.Clone(nil, nil)
			d.TabTitle = i18n.Text("Default Body Type")
		}
		d.TabIcon = res.BodyTypeSVG
		d.body.ResetTargetKeyPrefixes(d.targetMgr.NextPrefix)
		d.originalCRC = d.body.CRC64()
		d.Extensions = []string{library.BodyExt, library.BodyExtAlt}
		d.undoMgr = unison.NewUndoManager(100, func(err error) { jot.Error(err) })
		d.Loader = d.load
		d.Saver = d.save
		d.Resetter = d.reset
		d.ModifiedCallback = d.modified
		d.WillCloseCallback = d.willClose
		d.Setup(ws, dc, d.addToStartToolbar, nil, d.initContent)
	}
}

func (d *bodyDockable) UndoManager() *unison.UndoManager {
	return d.undoMgr
}

func (d *bodyDockable) modified() bool {
	modified := d.originalCRC != d.body.CRC64()
	d.applyButton.SetEnabled(modified)
	d.cancelButton.SetEnabled(modified)
	return modified
}

func (d *bodyDockable) willClose() bool {
	if d.promptForSave && d.originalCRC != d.body.CRC64() {
		switch unison.YesNoCancelDialog(fmt.Sprintf(i18n.Text("Apply changes made to\n%s?"), d.Title()), "") {
		case unison.ModalResponseDiscard:
		case unison.ModalResponseOK:
			d.apply()
		case unison.ModalResponseCancel:
			return false
		}
	}
	return true
}

func (d *bodyDockable) CloseWithGroup(other unison.Paneler) bool {
	return d.owner != nil && d.owner == other
}

func (d *bodyDockable) addToStartToolbar(toolbar *unison.Panel) {
	d.toolbar = toolbar
	d.applyButton = unison.NewSVGButton(res.CheckmarkSVG)
	d.applyButton.Tooltip = unison.NewTooltipWithText(i18n.Text("Apply Changes"))
	d.applyButton.SetEnabled(false)
	d.applyButton.ClickCallback = func() {
		d.apply()
		d.promptForSave = false
		d.AttemptClose()
	}
	toolbar.AddChild(d.applyButton)

	d.cancelButton = unison.NewSVGButton(res.NotSVG)
	d.cancelButton.Tooltip = unison.NewTooltipWithText(i18n.Text("Discard Changes"))
	d.cancelButton.SetEnabled(false)
	d.cancelButton.ClickCallback = func() {
		d.promptForSave = false
		d.AttemptClose()
	}
	toolbar.AddChild(d.cancelButton)
}

func (d *bodyDockable) initContent(content *unison.Panel) {
	d.content = content
	d.content.DataDragOverCallback = d.dataDragOver
	d.content.DataDragExitCallback = d.dataDragExit
	d.content.DataDragDropCallback = d.dataDragDrop
	d.content.DrawOverCallback = d.drawOver
	content.SetBorder(nil)
	content.SetLayout(&unison.FlexLayout{Columns: 1})
	content.AddChild(newBodyPanel(d))
}

func (d *bodyDockable) Entity() *gurps.Entity {
	if d.owner != nil {
		return d.owner.Entity()
	}
	return nil
}

func (d *bodyDockable) prepareUndo(title string) *unison.UndoEdit[*gurps.Body] {
	return &unison.UndoEdit[*gurps.Body]{
		ID:         unison.NextUndoID(),
		EditName:   title,
		UndoFunc:   func(e *unison.UndoEdit[*gurps.Body]) { d.applyBodyType(e.BeforeData) },
		RedoFunc:   func(e *unison.UndoEdit[*gurps.Body]) { d.applyBodyType(e.AfterData) },
		AbsorbFunc: func(e *unison.UndoEdit[*gurps.Body], other unison.Undoable) bool { return false },
		BeforeData: d.body.Clone(d.Entity(), nil),
	}
}

func (d *bodyDockable) finishAndPostUndo(undo *unison.UndoEdit[*gurps.Body]) {
	undo.AfterData = d.body.Clone(d.Entity(), nil)
	d.UndoManager().Add(undo)
}

func (d *bodyDockable) applyBodyType(bodyType *gurps.Body) {
	d.body = bodyType.Clone(d.Entity(), nil)
	d.sync()
}

func (d *bodyDockable) reset() {
	undo := d.prepareUndo(i18n.Text("Reset Body Type"))
	if d.owner != nil {
		d.body = settings.Global().Sheet.BodyType.Clone(d.Entity(), nil)
	} else {
		d.body = gurps.FactoryBody()
	}
	d.body.ResetTargetKeyPrefixes(d.targetMgr.NextPrefix)
	d.finishAndPostUndo(undo)
	d.sync()
}

func (d *bodyDockable) sync() {
	focusRefKey := d.targetMgr.CurrentFocusRef()
	scrollRoot := d.content.ScrollRoot()
	h, v := scrollRoot.Position()
	d.content.RemoveAllChildren()
	d.content.AddChild(newBodyPanel(d))
	d.MarkForLayoutRecursively()
	d.MarkForRedraw()
	d.ValidateLayout()
	d.MarkModified()
	d.targetMgr.ReacquireFocus(focusRefKey, d.toolbar, d.content)
	scrollRoot.SetPosition(h, v)
}

func (d *bodyDockable) load(fileSystem fs.FS, filePath string) error {
	bodyType, err := gurps.NewBodyFromFile(fileSystem, filePath)
	if err != nil {
		return err
	}
	bodyType.ResetTargetKeyPrefixes(d.targetMgr.NextPrefix)
	undo := d.prepareUndo(i18n.Text("Load Body Type"))
	d.body = bodyType
	d.finishAndPostUndo(undo)
	d.sync()
	return nil
}

func (d *bodyDockable) save(filePath string) error {
	return d.body.Save(filePath)
}

func (d *bodyDockable) apply() {
	d.Window().FocusNext() // Intentionally move the focus to ensure any pending edits are flushed
	if d.owner == nil {
		settings.Global().Sheet.BodyType = d.body.Clone(nil, nil)
		return
	}
	entity := d.owner.Entity()
	entity.SheetSettings.BodyType = d.body.Clone(entity, nil)
	for _, wnd := range unison.Windows() {
		if ws := workspace.FromWindow(wnd); ws != nil {
			ws.DocumentDock.RootDockLayout().ForEachDockContainer(func(dc *unison.DockContainer) bool {
				for _, one := range dc.Dockables() {
					if s, ok := one.(gurps.SheetSettingsResponder); ok {
						s.SheetSettingsUpdated(entity, true)
					}
				}
				return false
			})
		}
	}
}

func (d *bodyDockable) dataDragOver(where unison.Point, data map[string]any) bool {
	prevInDragOver := d.inDragOver
	dragInsert := d.dragInsert
	dragTarget := d.dragTarget
	d.inDragOver = false
	d.dragInsert = -1
	d.dragTargetBody = nil
	d.dragTarget = nil
	if dragData, ok := data[hitLocationDragDataKey]; ok {
		var dd *hitLocationPanel
		if dd, ok = dragData.(*hitLocationPanel); ok && dd.dockable == d {
			parent := dd.Parent()
			where = parent.PointFromRoot(d.content.PointToRoot(where))
			for i, child := range parent.Children() {
				rect := child.FrameRect()
				if rect.ContainsPoint(where) {
					d.dragTarget = parent
					if rect.CenterY() <= where.Y {
						d.dragInsert = i + 1
					} else {
						d.dragInsert = i
					}
					d.inDragOver = true
					break
				}
			}
		}
	}
	if prevInDragOver != d.inDragOver || dragInsert != d.dragInsert || dragTarget != d.dragTarget {
		d.MarkForRedraw()
	}
	return true
}

func (d *bodyDockable) dataDragExit() {
	d.inDragOver = false
	d.dragInsert = -1
	d.dragTargetBody = nil
	d.dragTarget = nil
	d.MarkForRedraw()
}

func (d *bodyDockable) dataDragDrop(_ unison.Point, data map[string]any) {
	if d.inDragOver && d.dragInsert != -1 {
		if dragData, ok := data[hitLocationDragDataKey]; ok {
			var dd *hitLocationPanel
			if dd, ok = dragData.(*hitLocationPanel); ok && dd.dockable == d && d.dragInsert != -1 {
				undo := d.prepareUndo(i18n.Text("Hit Location Drag"))
				table := dd.loc.OwningTable()
				i := slices.Index(table.Locations, dd.loc)
				table.Locations = slices.Delete(table.Locations, i, i+1)
				if i < d.dragInsert {
					d.dragInsert--
				}
				table.Locations = slices.Insert(table.Locations, d.dragInsert, dd.loc)
				table.Update(d.Entity())
				d.finishAndPostUndo(undo)
				d.sync()
			}
		}
	}
	d.dataDragExit()
}

func (d *bodyDockable) drawOver(gc *unison.Canvas, rect unison.Rect) {
	if d.inDragOver && d.dragInsert != -1 {
		children := d.dragTarget.Children()
		var y float32
		if d.dragInsert < len(children) {
			y = children[d.dragInsert].FrameRect().Y
		} else {
			y = children[len(children)-1].FrameRect().Bottom()
		}
		pt := d.content.PointFromRoot(d.dragTarget.PointToRoot(unison.Point{Y: y}))
		paint := unison.DropAreaColor.Paint(gc, rect, unison.Stroke)
		paint.SetStrokeWidth(2)
		r := d.content.RectFromRoot(d.dragTarget.RectToRoot(d.dragTarget.ContentRect(false)))
		gc.DrawLine(r.X, pt.Y, r.Right(), pt.Y, paint)
	}
}
