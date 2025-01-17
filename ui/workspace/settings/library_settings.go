/*
 * Copyright ©1998-2022 by Richard A. Wilkes. All rights reserved.
 *
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, version 2.0. If a copy of the MPL was not distributed with
 * this file, You can obtain one at http://mozilla.org/MPL/2.0/.
 *
 * This Source Code Form is "Incompatible With Secondary Licenses", as
 * defined by the Mozilla Public License, version 2.0.
 */

package settings

import (
	"context"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/richardwilkes/gcs/v5/model/gurps/gid"
	"github.com/richardwilkes/gcs/v5/model/library"
	"github.com/richardwilkes/gcs/v5/model/settings"
	"github.com/richardwilkes/gcs/v5/res"
	"github.com/richardwilkes/gcs/v5/ui/widget"
	"github.com/richardwilkes/gcs/v5/ui/workspace"
	"github.com/richardwilkes/toolbox/i18n"
	xfs "github.com/richardwilkes/toolbox/xio/fs"
	"github.com/richardwilkes/unison"
)

type librarySettingsDockable struct {
	Dockable
	library       *library.Library
	toolbar       *unison.Panel
	applyButton   *unison.Button
	cancelButton  *unison.Button
	nameField     *widget.StringField
	githubField   *widget.StringField
	repoField     *widget.StringField
	pathField     *widget.StringField
	name          string
	github        string
	repo          string
	path          string
	special       bool
	promptForSave bool
}

// ShowLibrarySettings the Library Settings view for a specific library.
func ShowLibrarySettings(lib *library.Library) {
	ws, dc, found := workspace.Activate(func(d unison.Dockable) bool {
		if settingsDockable, ok := d.(*librarySettingsDockable); ok && settingsDockable.library == lib {
			return true
		}
		return false
	})
	if !found && ws != nil {
		d := &librarySettingsDockable{
			library: lib,
			name:    lib.Title,
			github:  lib.GitHubAccountName,
			repo:    lib.RepoName,
			path:    lib.PathOnDisk,
			special: lib.IsMaster() || lib.IsUser(),
		}
		d.Self = d
		d.TabTitle = fmt.Sprintf(i18n.Text("Library Settings: %s"), lib.Title)
		d.TabIcon = res.SettingsSVG
		d.Setup(ws, dc, d.addToStartToolbar, nil, d.initContent)
		d.updateToolbar()
		d.nameField.RequestFocus()
	}
}

func (d *librarySettingsDockable) addToStartToolbar(toolbar *unison.Panel) {
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

func (d *librarySettingsDockable) initContent(content *unison.Panel) {
	content.SetLayout(&unison.FlexLayout{
		Columns:  2,
		HSpacing: unison.StdHSpacing,
		VSpacing: unison.StdVSpacing,
	})

	title := i18n.Text("Name")
	content.AddChild(widget.NewFieldLeadingLabel(title))
	d.nameField = widget.NewStringField(nil, "", title,
		func() string { return d.name },
		func(s string) {
			d.name = strings.TrimSpace(s)
			d.updateToolbar()
		})
	d.nameField.SetEnabled(!d.special)
	if !d.special {
		d.nameField.ValidateCallback = func() bool { return d.name != "" }
	}
	content.AddChild(d.nameField)

	title = i18n.Text("GitHub Account")
	content.AddChild(widget.NewFieldLeadingLabel(title))
	d.githubField = widget.NewStringField(nil, "", title,
		func() string { return d.github },
		func(s string) {
			d.github = s
			d.updateToolbar()
		})
	d.githubField.SetEnabled(!d.special)
	if !d.special {
		d.githubField.ValidateCallback = func() bool { return d.github != "" && !d.checkForSpecial() }
	}
	content.AddChild(d.githubField)

	title = i18n.Text("Repository")
	content.AddChild(widget.NewFieldLeadingLabel(title))
	d.repoField = widget.NewStringField(nil, "", title,
		func() string { return d.repo },
		func(s string) {
			d.repo = s
			d.updateToolbar()
		})
	d.repoField.SetEnabled(!d.special)
	if !d.special {
		d.repoField.ValidateCallback = func() bool { return d.repo != "" && !d.checkForSpecial() }
	}
	content.AddChild(d.repoField)

	title = i18n.Text("Path")
	content.AddChild(widget.NewFieldLeadingLabel(title))
	d.pathField = widget.NewStringField(nil, "", title,
		func() string { return d.path },
		func(s string) {
			d.path = s
			d.updateToolbar()
		})
	d.pathField.ValidateCallback = func() bool { return len(d.path) > 1 && filepath.IsAbs(d.path) }

	locateButton := unison.NewSVGButton(res.ClosedFolderSVG)
	locateButton.ClickCallback = d.choosePath

	wrapper := unison.NewPanel()
	wrapper.SetLayout(&unison.FlexLayout{
		Columns:  2,
		HSpacing: unison.StdHSpacing,
	})
	wrapper.SetLayoutData(&unison.FlexLayoutData{
		HAlign: unison.FillAlignment,
		HGrab:  true,
	})
	wrapper.AddChild(d.pathField)
	wrapper.AddChild(locateButton)

	content.AddChild(wrapper)

	content.AddChild(unison.NewPanel())
	info := unison.NewLabel()
	info.Text = i18n.Text("Once configured, the repository specified above will be scanned for release tags")
	info.SetBorder(unison.NewEmptyBorder(unison.Insets{Top: unison.StdVSpacing * 2}))
	content.AddChild(info)
	content.AddChild(unison.NewPanel())
	info = unison.NewLabel()
	info.Text = fmt.Sprintf(i18n.Text(`in the form "v%d.x.y" through "v%d.x.y", where x and y can be any numeric value.`), gid.MinimumLibraryVersion, gid.CurrentDataVersion)
	content.AddChild(info)
}

func (d *librarySettingsDockable) checkForSpecial() bool {
	lib := &library.Library{
		GitHubAccountName: d.github,
		RepoName:          d.repo,
	}
	return lib.IsMaster() || lib.IsUser()
}

func (d *librarySettingsDockable) choosePath() {
	dlg := unison.NewOpenDialog()
	dlg.SetAllowsMultipleSelection(false)
	dlg.SetResolvesAliases(true)
	dlg.SetCanChooseDirectories(true)
	dlg.SetCanChooseFiles(false)
	if xfs.IsDir(d.path) {
		dlg.SetInitialDirectory(d.path)
	} else {
		dlg.SetInitialDirectory(settings.Global().LastDir(settings.DefaultLastDirKey))
	}
	if dlg.RunModal() {
		p, err := filepath.Abs(dlg.Path())
		if err != nil {
			unison.ErrorDialogWithMessage(i18n.Text("Unable to resolve absolute path"), dlg.Path())
		} else {
			d.pathField.SetText(p)
		}
		d.pathField.SelectAll()
		d.pathField.RequestFocus()
	}
}

func (d *librarySettingsDockable) updateToolbar() {
	d.nameField.Validate()
	d.githubField.Validate()
	d.repoField.Validate()
	d.pathField.Validate()
	modified := d.library.Title != d.name || d.library.GitHubAccountName != d.github ||
		d.library.RepoName != d.repo || d.library.PathOnDisk != d.path
	d.applyButton.SetEnabled(modified &&
		!(d.nameField.Invalid() || d.githubField.Invalid() || d.repoField.Invalid() || d.pathField.Invalid()))
	d.cancelButton.SetEnabled(modified)
}

func (d *librarySettingsDockable) apply() {
	wnd := d.Window()
	wnd.FocusNext() // Intentionally move the focus to ensure any pending edits are flushed
	libs := settings.Global().LibrarySet
	delete(libs, d.library.Key())
	d.library.Title = d.name
	d.library.GitHubAccountName = d.github
	d.library.RepoName = d.repo
	libs[d.library.Key()] = d.library
	if err := d.library.SetPath(d.path); err != nil {
		unison.ErrorDialogWithError(i18n.Text("Unable to update library location"), err)
	}
	workspace.FromWindowOrAny(wnd).Navigator.Reload()
	go checkForLibraryUpgrade(d.library)
}

func checkForLibraryUpgrade(lib *library.Library) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute*5)
	defer cancel()
	lib.CheckForAvailableUpgrade(ctx, &http.Client{})
}
