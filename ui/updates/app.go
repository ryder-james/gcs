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

package updates

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/richardwilkes/gcs/v5/constants"
	"github.com/richardwilkes/gcs/v5/model/library"
	"github.com/richardwilkes/gcs/v5/model/settings"
	"github.com/richardwilkes/gcs/v5/res"
	"github.com/richardwilkes/gcs/v5/ui/widget"
	"github.com/richardwilkes/toolbox/cmdline"
	"github.com/richardwilkes/toolbox/desktop"
	"github.com/richardwilkes/toolbox/i18n"
	"github.com/richardwilkes/toolbox/log/jot"
	"github.com/richardwilkes/toolbox/txt"
	"github.com/richardwilkes/unison"
)

type appUpdater struct {
	lock     sync.RWMutex
	result   string
	releases []library.Release
	updating bool
}

var appUpdate appUpdater

func (u *appUpdater) Reset() bool {
	u.lock.Lock()
	defer u.lock.Unlock()
	if u.updating {
		return false
	}
	u.result = fmt.Sprintf(i18n.Text("Checking for %s updates…"), cmdline.AppName)
	u.releases = nil
	u.updating = true
	return true
}

func (u *appUpdater) Result() (title string, releases []library.Release, updating bool) {
	u.lock.RLock()
	defer u.lock.RUnlock()
	return u.result, u.releases, u.updating
}

func (u *appUpdater) SetResult(str string) {
	u.lock.Lock()
	u.result = str
	u.updating = false
	u.lock.Unlock()
}

func (u *appUpdater) SetReleases(releases []library.Release) {
	u.lock.Lock()
	u.result = fmt.Sprintf(i18n.Text("%s v%s is available!"), cmdline.AppName, releases[0].Version)
	u.releases = releases
	u.updating = false
	u.lock.Unlock()
}

// CheckForAppUpdates initiates a fresh check for application updates.
func CheckForAppUpdates() {
	if cmdline.AppVersion == "0.0" {
		appUpdate.SetResult(fmt.Sprintf(i18n.Text("Development versions don't look for %s updates"), cmdline.AppName))
		return
	}
	if appUpdate.Reset() {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute*5)
			defer cancel()
			releases, err := library.LoadReleases(ctx, &http.Client{}, "richardwilkes", "gcs", cmdline.AppVersion,
				func(version, notes string) bool {
					// Don't bother showing changes from before 5.0.0, since those were the Java version
					return txt.NaturalLess(version, "5.0.0", true)
				})
			if err != nil {
				appUpdate.SetResult(fmt.Sprintf(i18n.Text("Unable to access the %s update site"), cmdline.AppName))
				jot.Error(err)
				return
			}
			if len(releases) == 0 || releases[0].Version == cmdline.AppVersion {
				appUpdate.SetResult(fmt.Sprintf(i18n.Text("No %s updates are available"), cmdline.AppName))
				return
			}
			appUpdate.SetReleases(releases)
			unison.InvokeTask(NotifyOfAppUpdate)
		}()
	}
}

// NotifyOfAppUpdate notifies the user of the available update.
func NotifyOfAppUpdate() {
	if title, releases, _ := appUpdate.Result(); releases != nil {
		var buffer strings.Builder
		fmt.Fprintf(&buffer, "# %s\n", title)
		for i, rel := range releases {
			if i != 0 {
				buffer.WriteString("---\n")
			}
			fmt.Fprintf(&buffer, "## Release Notes for %s v%s\n", cmdline.AppName, rel.Version)
			buffer.WriteString(rel.Notes)
			buffer.WriteByte('\n')
		}

		md := widget.NewMarkdown()
		md.SetContent(buffer.String(), 0)

		scroll := unison.NewScrollPanel()
		scroll.SetContent(md, unison.UnmodifiedBehavior, unison.UnmodifiedBehavior)

		dialog, err := unison.NewDialog(
			&unison.DrawableSVG{
				SVG:  res.DownloadSVG,
				Size: unison.NewSize(48, 48),
			},
			unison.DefaultLabelTheme.OnBackgroundInk, scroll,
			[]*unison.DialogButtonInfo{
				unison.NewCancelButtonInfo(),
				unison.NewOKButtonInfoWithTitle(i18n.Text("Download")),
			})
		if err != nil {
			jot.Error(err)
			return
		}
		settings.Global().LastSeenGCSVersion = releases[0].Version
		if dialog.RunModal() == unison.ModalResponseOK {
			if err = desktop.Open("https://" + constants.WebSiteDomain); err != nil {
				unison.ErrorDialogWithError(i18n.Text("Unable to open web page for download"), err)
			}
		}
	}
}

// AppUpdateResult returns the current results of any outstanding app update check.
func AppUpdateResult() (title string, releases []library.Release, updating bool) {
	return appUpdate.Result()
}
