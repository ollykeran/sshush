// Package gui is a Fyne proof-of-concept desktop UI that reuses the same internal
// packages as the TUI (agent, sshushd, config, keys, etc.).
package gui

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	fynetheme "fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/ollykeran/sshush/internal/agent"
	"github.com/ollykeran/sshush/internal/config"
	"github.com/ollykeran/sshush/internal/editcomment"
	"github.com/ollykeran/sshush/internal/keys"
	"github.com/ollykeran/sshush/internal/runtime"
	"github.com/ollykeran/sshush/internal/sshushd"
	sshushtheme "github.com/ollykeran/sshush/internal/theme"
	"github.com/ollykeran/sshush/internal/utils"
	ssh "golang.org/x/crypto/ssh"
	sshagent "golang.org/x/crypto/ssh/agent"
)

type uiState struct {
	fyneApp           fyne.App
	win               fyne.Window
	configPath        string // path for theme write + SSHUSH_CONFIG when file exists
	configPathForFile string // always ConfigPathDefault for LoadThemeFromPath
	cfg               *config.Config
	socketPath        string
	th                sshushtheme.Theme
	uiSizeScale       float32 // theme.Size multiplier (zoom); persisted only for this session

	// Agent
	agentStatus *widget.Label // status bar: feedback / operations (colour via Importance)
	daemonLabel *widget.Label // status bar: agent state (colour via Importance)
	keyList     *widget.List
	keyRows         []keyRow
	keySelected     int
	discovered      *widget.List
	discPaths       []string
	discSelected    int
	exportPub       *widget.Label
	exportKeyType   *widget.Label
	exportSource    *widget.Label
	exportAgentLK   *widget.List
	exportAgentRows []keyRow
	exportSel       int
}

type keyRow struct {
	fp      string
	display string
	raw     *sshagent.Key
}

const (
	defaultGUISizeScale float32 = 1.15
	guiZoomStep         float32 = 0.05
)

// DESIGN SPECIFICATION (desktop Fyne, sshush theme)
// Purpose: One top status strip for agent state (colour-coded) and shared feedback messages; lists below for keys.
// Direction: Industrial/utilitarian; monospace for fingerprint columns.
// Palette: sshush theme; Success/Warning/Danger/Medium on status lines via widget.Importance.
// Layout: Full-width header strip then controls; split lists fill remaining height.

func sectionHeading(text string) *widget.Label {
	l := widget.NewLabel(text)
	l.TextStyle = fyne.TextStyle{Bold: true}
	return l
}

func subtleHint(text string) *widget.Label {
	l := widget.NewLabel(text)
	l.Importance = widget.LowImportance
	l.Wrapping = fyne.TextWrapWord
	return l
}

func paddedTab(c fyne.CanvasObject) fyne.CanvasObject {
	return container.NewPadded(c)
}

func newKeyRowLabel() fyne.CanvasObject {
	l := widget.NewLabel("")
	l.TextStyle = fyne.TextStyle{Monospace: true}
	l.Wrapping = fyne.TextWrapWord
	return l
}

func (st *uiState) applyGUITheme() {
	if st.fyneApp == nil {
		return
	}
	st.fyneApp.Settings().SetTheme(NewFyneThemeScaled(st.th, st.uiSizeScale))
}

// fileDialogSizeForParent sizes the Fyne file picker relative to the parent canvas with inset padding.
func fileDialogSizeForParent(parent fyne.Window) fyne.Size {
	const minW, minH float32 = 640, 520
	if parent == nil {
		return fyne.NewSize(900, 680)
	}
	c := parent.Canvas()
	if c == nil {
		return fyne.NewSize(900, 680)
	}
	inset := fynetheme.Padding() * 4
	sz := c.Size()
	w := sz.Width - inset*2
	h := sz.Height - inset*2
	if w < minW {
		w = minW
	}
	if h < minH {
		h = minH
	}
	return fyne.NewSize(w, h)
}

func (st *uiState) showFileOpen(callback func(fyne.URIReadCloser, error)) {
	d := dialog.NewFileOpen(callback, st.win)
	d.Resize(fileDialogSizeForParent(st.win))
	d.Show()
}

func (st *uiState) showFileSave(initialName string, callback func(fyne.URIWriteCloser, error)) {
	d := dialog.NewFileSave(callback, st.win)
	if initialName != "" {
		d.SetFileName(initialName)
	}
	d.Resize(fileDialogSizeForParent(st.win))
	d.Show()
}

// Run starts the Fyne desktop application (blocking until the window closes).
func Run() error {
	st := loadState()
	a := app.NewWithID("io.github.ollykeran.sshush.gui")
	st.fyneApp = a
	st.applyGUITheme()

	w := a.NewWindow("sshush")
	st.win = w

	tabs := container.NewAppTabs(
		container.NewTabItem("Agent", paddedTab(buildAgentUI(st))),
		container.NewTabItem("Theme", paddedTab(buildThemeUI(st))),
		container.NewTabItem("Create", paddedTab(buildCreateUI(st))),
		container.NewTabItem("Edit", paddedTab(buildEditUI(st))),
		container.NewTabItem("Export", paddedTab(buildExportUI(st))),
	)
	tabs.SetTabLocation(container.TabLocationTop)

	w.SetContent(tabs)
	w.Resize(fyne.NewSize(1000, 760))
	w.ShowAndRun()
	return nil
}

func loadState() *uiState {
	cp := runtime.ConfigPathDefault()
	st := &uiState{
		configPathForFile: cp,
		th:                config.LoadThemeFromPath(cp),
		uiSizeScale:       defaultGUISizeScale,
		keySelected:       -1,
		discSelected:      -1,
		exportSel:         -1,
	}
	if _, err := os.Stat(cp); err == nil {
		st.configPath = cp
		if c, err := config.LoadConfig(cp); err == nil {
			st.cfg = &c
		}
	}
	sock, err := runtime.SocketPathForSSHushGUI(st.cfg)
	if err == nil {
		st.socketPath = sock
	}
	return st
}

func (st *uiState) applyDaemonRunning(running bool) {
	if st.daemonLabel == nil {
		return
	}
	st.daemonLabel.TextStyle = fyne.TextStyle{Bold: true}
	st.daemonLabel.Wrapping = fyne.TextWrapWord
	if st.socketPath == "" {
		st.daemonLabel.SetText("Agent: unavailable (no socket path)")
		st.daemonLabel.Importance = widget.DangerImportance
		st.daemonLabel.Refresh()
		return
	}
	if running {
		st.daemonLabel.SetText("Agent: running (sshushd)")
		st.daemonLabel.Importance = widget.SuccessImportance
	} else {
		st.daemonLabel.SetText("Agent: stopped")
		st.daemonLabel.Importance = widget.WarningImportance
	}
	st.daemonLabel.Refresh()
}

// setAgentBarMessage sets the second line of the Agent tab status bar (operations and list refresh feedback).
func (st *uiState) setAgentBarMessage(msg string, imp widget.Importance) {
	if st.agentStatus == nil {
		return
	}
	st.agentStatus.Text = msg
	st.agentStatus.Importance = imp
	st.agentStatus.Refresh()
}

func buildThemeUI(st *uiState) fyne.CanvasObject {
	current := widget.NewLabel("")
	names := sshushtheme.PresetNamesOrdered()
	sel := widget.NewSelect(names, nil)
	preset := ""
	for _, n := range names {
		if t, ok := sshushtheme.Presets[n]; ok && themesEqual(t, st.th) {
			preset = n
			break
		}
	}
	if preset != "" {
		sel.SetSelected(preset)
		current.SetText("Current: " + preset)
	} else {
		current.SetText("Current: custom (from config)")
	}

	apply := widget.NewButton("Apply preset", func() {
		name := sel.Selected
		if name == "" {
			dialog.ShowInformation("Theme", "Select a preset.", st.win)
			return
		}
		if st.configPath == "" {
			dialog.ShowError(errors.New("no config file at "+utils.DisplayPath(st.configPathForFile)+"; create one first (e.g. sshush config generate)"), st.win)
			return
		}
		if err := config.WriteThemeToPath(st.configPath, name, nil); err != nil {
			dialog.ShowError(err, st.win)
			return
		}
		st.th = config.LoadThemeFromPath(st.configPathForFile)
		st.applyGUITheme()
		current.SetText("Current: " + name)
		dialog.ShowInformation("Theme", "Saved "+name+" to config.", st.win)
	})

	card := widget.NewCard(
		"Theme preset",
		"Updates the [theme] section in your sshush config (same as the CLI).",
		container.NewVBox(current, sel, apply),
	)

	zoomLabel := widget.NewLabel("")
	refreshZoomLabel := func() {
		zoomLabel.SetText(fmt.Sprintf("Interface scale: %d%% (text, buttons, spacing)", int(st.uiSizeScale*100+0.5)))
	}
	refreshZoomLabel()
	zoomOut := widget.NewButton("Smaller", func() {
		st.uiSizeScale = ClampUIScale(st.uiSizeScale - guiZoomStep)
		st.applyGUITheme()
		refreshZoomLabel()
	})
	zoomIn := widget.NewButton("Larger", func() {
		st.uiSizeScale = ClampUIScale(st.uiSizeScale + guiZoomStep)
		st.applyGUITheme()
		refreshZoomLabel()
	})
	zoomReset := widget.NewButton("Default scale", func() {
		st.uiSizeScale = defaultGUISizeScale
		st.applyGUITheme()
		refreshZoomLabel()
	})
	zoomRow := container.NewHBox(zoomOut, zoomIn, zoomReset, zoomLabel)
	zoomCard := widget.NewCard(
		"Zoom and readability",
		"Scales the whole UI. File picker dialogs match the main window size.",
		container.NewVBox(zoomRow, subtleHint("Range about 85% to 160%. OS display scaling still applies on top.")),
	)

	return container.NewVBox(subtleHint("Requires an existing config file at "+utils.DisplayPath(st.configPathForFile)+"."), card, zoomCard)
}

func themesEqual(a, b sshushtheme.Theme) bool {
	return a.Text == b.Text && a.Focus == b.Focus && a.Accent == b.Accent && a.Error == b.Error && a.Warning == b.Warning
}

func buildAgentUI(st *uiState) fyne.CanvasObject {
	st.agentStatus = widget.NewLabel("")
	st.agentStatus.Wrapping = fyne.TextWrapWord
	st.daemonLabel = widget.NewLabel("")
	st.daemonLabel.Wrapping = fyne.TextWrapWord
	st.keyRows = nil
	st.keyList = widget.NewList(
		func() int { return len(st.keyRows) },
		func() fyne.CanvasObject {
			return newKeyRowLabel()
		},
		func(id widget.ListItemID, obj fyne.CanvasObject) {
			if id < 0 || id >= len(st.keyRows) {
				return
			}
			obj.(*widget.Label).SetText(st.keyRows[id].display)
		},
	)
	st.keyList.OnSelected = func(id widget.ListItemID) {
		st.keySelected = id
	}
	st.keyList.OnUnselected = func(_ widget.ListItemID) {
		st.keySelected = -1
	}

	refresh := func() {
		go st.refreshAgentKeys()
	}

	start := widget.NewButton("Start", func() {
		go func() {
			err := sshushd.StartDaemon(st.configPath, st.socketPath)
			fyne.Do(func() {
				if err != nil {
					if err.Error() == "already running" {
						st.setAgentBarMessage("already running", widget.MediumImportance)
					} else {
						st.setAgentBarMessage(err.Error(), widget.DangerImportance)
					}
				} else {
					st.setAgentBarMessage("started", widget.SuccessImportance)
				}
				st.applyDaemonRunning(sshushd.CheckAlreadyRunning(st.socketPath))
				refresh()
			})
		}()
	})
	stop := widget.NewButton("Stop", func() {
		go func() {
			pid := runtime.PidFilePath()
			var msg string
			var errFlag bool
			if _, err := os.Stat(pid); os.IsNotExist(err) {
				msg, errFlag = "agent not running", true
			} else if err := sshushd.StopDaemon(pid); err != nil {
				msg, errFlag = "stop failed", true
			} else {
				msg = "stopped"
			}
			fyne.Do(func() {
				if errFlag {
					st.setAgentBarMessage(msg, widget.DangerImportance)
				} else {
					st.setAgentBarMessage(msg, widget.MediumImportance)
				}
				st.applyDaemonRunning(sshushd.CheckAlreadyRunning(st.socketPath))
				refresh()
			})
		}()
	})
	reload := widget.NewButton("Reload", func() {
		go func() {
			err := sshushd.ReloadDaemon(st.configPath, st.socketPath, runtime.PidFilePath())
			fyne.Do(func() {
				if err != nil {
					st.setAgentBarMessage(err.Error(), widget.DangerImportance)
				} else {
					st.setAgentBarMessage("reloaded", widget.SuccessImportance)
				}
				st.applyDaemonRunning(sshushd.CheckAlreadyRunning(st.socketPath))
				refresh()
			})
		}()
	})

	addFile := widget.NewButton("Add key (file…)", func() {
		st.showFileOpen(func(reader fyne.URIReadCloser, err error) {
			if err != nil {
				dialog.ShowError(err, st.win)
				return
			}
			if reader == nil {
				return
			}
			path := reader.URI().Path()
			_ = reader.Close()
			go func() {
				e := agent.AddKeyToSocketFromPath(st.socketPath, path)
				fyne.Do(func() {
					if e != nil {
						st.setAgentBarMessage("add failed: "+e.Error(), widget.DangerImportance)
					} else {
						st.setAgentBarMessage("key added: "+utils.DisplayPath(path), widget.SuccessImportance)
					}
					refresh()
				})
			}()
		})
	})

	remove := widget.NewButton("Remove selected", func() {
		if st.keySelected < 0 || st.keySelected >= len(st.keyRows) {
			st.setAgentBarMessage("select a key first", widget.WarningImportance)
			return
		}
		fp := st.keyRows[st.keySelected].fp
		go func() {
			removed, e := agent.RemoveKeyFromSocketByFingerprint(st.socketPath, fp)
			fyne.Do(func() {
				if e != nil {
					st.setAgentBarMessage("agent not running", widget.DangerImportance)
					return
				}
				if !removed {
					st.setAgentBarMessage("key not found", widget.DangerImportance)
					return
				}
				st.setAgentBarMessage("key removed", widget.SuccessImportance)
				st.keySelected = -1
				refresh()
			})
		}()
	})

	lock := widget.NewButton("Lock…", func() {
		pass := widget.NewPasswordEntry()
		dialog.ShowForm("Lock agent", "Lock", "Cancel", []*widget.FormItem{
			{Text: "Passphrase", Widget: pass},
		}, func(ok bool) {
			if !ok {
				return
			}
			go func() {
				e := agent.LockSocket(st.socketPath, []byte(pass.Text))
				fyne.Do(func() {
					if e != nil {
						st.setAgentBarMessage(e.Error(), widget.DangerImportance)
					} else {
						st.setAgentBarMessage("locked", widget.SuccessImportance)
					}
				})
			}()
		}, st.win)
	})
	unlock := widget.NewButton("Unlock…", func() {
		pass := widget.NewPasswordEntry()
		dialog.ShowForm("Unlock agent", "Unlock", "Cancel", []*widget.FormItem{
			{Text: "Passphrase", Widget: pass},
		}, func(ok bool) {
			if !ok {
				return
			}
			go func() {
				e := agent.UnlockSocket(st.socketPath, []byte(pass.Text))
				fyne.Do(func() {
					if e != nil {
						st.setAgentBarMessage(e.Error(), widget.DangerImportance)
					} else {
						st.setAgentBarMessage("unlocked", widget.SuccessImportance)
					}
				})
			}()
		}, st.win)
	})

	st.discPaths = nil
	st.discovered = widget.NewList(
		func() int { return len(st.discPaths) },
		func() fyne.CanvasObject { return widget.NewLabel("path") },
		func(id widget.ListItemID, obj fyne.CanvasObject) {
			if id >= 0 && id < len(st.discPaths) {
				obj.(*widget.Label).SetText(utils.DisplayPath(st.discPaths[id]))
			}
		},
	)
	st.discovered.OnSelected = func(id widget.ListItemID) { st.discSelected = id }

	refreshDisc := widget.NewButton("Refresh discovered", func() {
		st.discPaths = utils.DiscoverKeyPaths([]string{}, true, true, false)
		st.discovered.Refresh()
		st.discovered.UnselectAll()
		st.discSelected = -1
	})

	addDisc := widget.NewButton("Add selected discovered key", func() {
		if st.discSelected < 0 || st.discSelected >= len(st.discPaths) {
			st.setAgentBarMessage("select a discovered path", widget.WarningImportance)
			return
		}
		path := st.discPaths[st.discSelected]
		go func() {
			e := agent.AddKeyToSocketFromPath(st.socketPath, path)
			fyne.Do(func() {
				if e != nil {
					st.setAgentBarMessage("add failed: "+e.Error(), widget.DangerImportance)
				} else {
					st.setAgentBarMessage("key added: "+utils.DisplayPath(path), widget.SuccessImportance)
				}
				refresh()
			})
		}()
	})

	daemonCtrl := container.NewHBox(start, stop, reload)
	keyCtrl := container.NewHBox(addFile, remove, lock, unlock)

	statusBG := canvas.NewRectangle(fynetheme.Color(fynetheme.ColorNameHeaderBackground))
	statusPad := container.NewPadded(container.NewVBox(st.daemonLabel, st.agentStatus))
	statusBar := container.NewStack(statusBG, statusPad)

	subKeys := widget.NewLabel("SHA256 fingerprint and comment")
	subKeys.Importance = widget.LowImportance
	keysBox := container.NewBorder(
		container.NewVBox(
			sectionHeading("Keys in agent"),
			subKeys,
		), nil, nil, nil,
		container.NewScroll(st.keyList),
	)

	discHeader := container.NewVBox(
		sectionHeading("Discovered private key files"),
		container.NewHBox(refreshDisc, addDisc),
	)
	discBox := container.NewBorder(discHeader, nil, nil, nil, container.NewScroll(st.discovered))

	split := container.NewHSplit(keysBox, discBox)
	split.SetOffset(0.55)

	header := container.NewVBox(
		statusBar,
		widget.NewSeparator(),
		sectionHeading("Daemon"),
		daemonCtrl,
		widget.NewSeparator(),
		sectionHeading("Agent actions"),
		keyCtrl,
		widget.NewSeparator(),
	)

	st.applyDaemonRunning(sshushd.CheckAlreadyRunning(st.socketPath))
	refresh()
	st.discPaths = utils.DiscoverKeyPaths([]string{}, true, true, false)
	st.discovered.Refresh()

	return container.NewBorder(header, nil, nil, nil, split)
}

func (st *uiState) refreshAgentKeys() {
	if st.socketPath == "" {
		fyne.Do(func() {
			st.keyRows = nil
			st.keyList.Refresh()
			st.setAgentBarMessage("no sshush socket path (set [agent].socket_path or XDG_RUNTIME_DIR)", widget.DangerImportance)
		})
		return
	}
	keysList, err := agent.ListKeysFromSocket(st.socketPath)
	fyne.Do(func() {
		if err != nil {
			st.keyRows = nil
			st.keyList.Refresh()
			msg := err.Error()
			if strings.Contains(msg, "no such file or directory") || strings.Contains(msg, "connection refused") {
				msg = "agent not running (no socket). Click Start or run: sshush / sshushd"
			}
			st.setAgentBarMessage(msg, widget.DangerImportance)
			return
		}
		rows := make([]keyRow, len(keysList))
		for i, k := range keysList {
			fp := ssh.FingerprintSHA256(k)
			rows[i] = keyRow{
				fp:      fp,
				display: fmt.Sprintf("%s  %s  %s", k.Type(), fp, k.Comment),
				raw:     k,
			}
		}
		st.keyRows = rows
		st.keyList.Refresh()
		if len(rows) == 0 {
			st.setAgentBarMessage("no keys loaded", widget.MediumImportance)
		} else {
			st.setAgentBarMessage(fmt.Sprintf("%d key(s) loaded", len(rows)), widget.SuccessImportance)
		}
	})
}

func buildCreateUI(st *uiState) fyne.CanvasObject {
	status := widget.NewLabel("")
	keyTypes := []string{"ed25519", "rsa", "ecdsa"}
	typeSel := widget.NewRadioGroup(keyTypes, nil)
	typeSel.Horizontal = true
	typeSel.Required = true
	typeSel.Selected = "ed25519"

	rsaOpts := widget.NewRadioGroup([]string{"2048", "3072", "4096"}, nil)
	rsaOpts.Horizontal = true
	rsaOpts.Selected = "4096"
	ecdsaOpts := widget.NewRadioGroup([]string{"256", "384", "521"}, nil)
	ecdsaOpts.Horizontal = true
	ecdsaOpts.Selected = "256"

	optsBox := container.NewVBox(widget.NewLabel("RSA/ECDSA size"), rsaOpts, ecdsaOpts)
	optsBox.Hide()

	typeSel.OnChanged = func(s string) {
		if s == "rsa" || s == "ecdsa" {
			optsBox.Show()
			if s == "rsa" {
				rsaOpts.Show()
				ecdsaOpts.Hide()
			} else {
				rsaOpts.Hide()
				ecdsaOpts.Show()
			}
		} else {
			optsBox.Hide()
		}
	}

	comment := widget.NewEntry()
	comment.SetText(keys.DefaultComment())
	dir := widget.NewEntry()
	home, _ := os.UserHomeDir()
	dir.SetText(filepath.Join(home, ".ssh"))
	filename := widget.NewEntry()
	filename.SetText("id_ed25519")

	gen := widget.NewButton("Generate keypair", func() {
		kt := typeSel.Selected
		if kt == "" {
			status.Text = "select a key type"
			status.Importance = widget.DangerImportance
			status.Refresh()
			return
		}
		bits := 0
		switch kt {
		case "rsa":
			fmt.Sscanf(rsaOpts.Selected, "%d", &bits)
		case "ecdsa":
			fmt.Sscanf(ecdsaOpts.Selected, "%d", &bits)
		}
		com := strings.TrimSpace(comment.Text)
		if com == "" {
			com = keys.DefaultComment()
		}
		d := strings.TrimSpace(dir.Text)
		fn := strings.TrimSpace(filename.Text)
		if fn == "" {
			fn = "id_" + kt
		}
		go func() {
			privPEM, pubAuth, err := keys.Generate(kt, bits, com)
			if err != nil {
				fyne.Do(func() {
					status.Text = err.Error()
					status.Importance = widget.DangerImportance
					status.Refresh()
				})
				return
			}
			err = keys.SavePair(d, fn, privPEM, pubAuth)
			fyne.Do(func() {
				if err != nil {
					status.Text = err.Error()
					status.Importance = widget.DangerImportance
				} else {
					status.Text = "saved: " + utils.DisplayPath(filepath.Join(d, fn))
					status.Importance = widget.SuccessImportance
				}
				status.Refresh()
			})
		}()
	})

	return container.NewVBox(
		subtleHint("Creates an OpenSSH keypair on disk (same as the TUI Create tab)."),
		sectionHeading("Algorithm"),
		widget.NewForm(
			widget.NewFormItem("Type", typeSel),
		),
		optsBox,
		sectionHeading("Output"),
		widget.NewForm(
			widget.NewFormItem("Comment", comment),
			widget.NewFormItem("Directory", dir),
			widget.NewFormItem("Filename", filename),
		),
		gen,
		widget.NewSeparator(),
		status,
	)
}

func buildEditUI(st *uiState) fyne.CanvasObject {
	status := widget.NewLabel("")
	pathLabel := widget.NewLabel("No file loaded")
	pathLabel.Importance = widget.LowImportance
	comment := widget.NewMultiLineEntry()
	comment.SetMinRowsVisible(6)

	var loadedPath string
	var rawKey interface{}

	loadBtn := widget.NewButton("Open private key…", func() {
		st.showFileOpen(func(reader fyne.URIReadCloser, err error) {
			if err != nil {
				dialog.ShowError(err, st.win)
				return
			}
			if reader == nil {
				return
			}
			p := reader.URI().Path()
			_ = reader.Close()
			go func() {
				parsed, raw, _, err := keys.LoadKeyMaterial(p)
				fyne.Do(func() {
					if err != nil {
						if strings.Contains(err.Error(), "encrypted keys not supported") {
							status.Text = utils.DisplayPath(p) + ": is not an unencrypted OpenSSH key"
						} else {
							status.Text = err.Error()
						}
						status.Importance = widget.DangerImportance
						status.Refresh()
						return
					}
					loadedPath = p
					rawKey = raw
					pathLabel.Importance = widget.MediumImportance
					pathLabel.TextStyle = fyne.TextStyle{Bold: true}
					pathLabel.SetText(utils.DisplayPath(p))
					comment.SetText(parsed.Comment)
					status.Text = "loaded"
					status.Importance = widget.MediumImportance
					status.Refresh()
				})
			}()
		})
	})

	extEditor := widget.NewButton("Edit comment in $EDITOR", func() {
		if rawKey == nil {
			status.Text = "load a key first"
			status.Importance = widget.DangerImportance
			status.Refresh()
			return
		}
		cur := strings.TrimSpace(comment.Text)
		ed := runtime.ResolveEditor("")
		go func() {
			next, err := editcomment.EditCommentWithEditor(cur, ed)
			fyne.Do(func() {
				if err != nil {
					if errors.Is(err, editcomment.ErrExitedWithoutSaving) {
						status.Text = "no changes from editor"
						status.Importance = widget.MediumImportance
					} else {
						status.Text = err.Error()
						status.Importance = widget.DangerImportance
					}
					status.Refresh()
					return
				}
				comment.SetText(next)
				status.Text = "comment updated from editor"
				status.Importance = widget.SuccessImportance
				status.Refresh()
			})
		}()
	})

	save := widget.NewButton("Save", func() {
		if rawKey == nil || loadedPath == "" {
			status.Text = "load a key first"
			status.Importance = widget.DangerImportance
			status.Refresh()
			return
		}
		com := strings.TrimSpace(comment.Text)
		if com == "" {
			status.Text = "comment cannot be empty"
			status.Importance = widget.DangerImportance
			status.Refresh()
			return
		}
		go func() {
			err := keys.SaveWithComment(rawKey, com, loadedPath)
			fyne.Do(func() {
				if err != nil {
					status.Text = err.Error()
					status.Importance = widget.DangerImportance
				} else {
					status.Text = "saved"
					status.Importance = widget.SuccessImportance
				}
				status.Refresh()
			})
		}()
	})

	actions := container.NewHBox(loadBtn, extEditor)

	return container.NewVBox(
		subtleHint("Unencrypted OpenSSH private keys only. Optional: edit the comment in $EDITOR."),
		sectionHeading("Private key"),
		pathLabel,
		actions,
		widget.NewForm(widget.NewFormItem("Comment", comment)),
		save,
		widget.NewSeparator(),
		status,
	)
}

func buildExportUI(st *uiState) fyne.CanvasObject {
	st.exportPub = widget.NewLabel("")
	st.exportKeyType = widget.NewLabel("")
	st.exportSource = widget.NewLabel("")
	st.exportPub.Wrapping = fyne.TextWrapWord

	st.exportAgentRows = nil
	st.exportAgentLK = widget.NewList(
		func() int { return len(st.exportAgentRows) },
		func() fyne.CanvasObject { return newKeyRowLabel() },
		func(id widget.ListItemID, obj fyne.CanvasObject) {
			if id >= 0 && id < len(st.exportAgentRows) {
				obj.(*widget.Label).SetText(st.exportAgentRows[id].display)
			}
		},
	)
	st.exportAgentLK.OnSelected = func(id widget.ListItemID) {
		st.exportSel = id
		if id < 0 || id >= len(st.exportAgentRows) {
			return
		}
		k := st.exportAgentRows[id].raw
		if k == nil {
			return
		}
		// Match TUI export tab: type + fingerprint + comment (same row join as handleAgentTable).
		line := k.Type() + " " + ssh.FingerprintSHA256(k) + " " + k.Comment
		st.exportPub.SetText(strings.TrimSpace(line))
		st.exportKeyType.SetText(k.Type())
		st.exportSource.SetText("agent")
	}

	status := widget.NewLabel("")

	loadFile := widget.NewButton("Load from file…", func() {
		st.showFileOpen(func(reader fyne.URIReadCloser, err error) {
			if err != nil {
				dialog.ShowError(err, st.win)
				return
			}
			if reader == nil {
				return
			}
			p := reader.URI().Path()
			_ = reader.Close()
			go func() {
				parsed, _, signer, err := keys.LoadKeyMaterial(p)
				fyne.Do(func() {
					if err != nil {
						if strings.Contains(err.Error(), "encrypted keys not supported") {
							status.Text = "not an unencrypted OpenSSH key"
						} else {
							status.Text = err.Error()
						}
						status.Importance = widget.DangerImportance
						status.Refresh()
						return
					}
					st.exportPub.SetText(strings.TrimSpace(keys.FormatPublicKey(signer, parsed.Comment)))
					st.exportKeyType.SetText(parsed.KeyType)
					st.exportSource.SetText(utils.DisplayPath(p))
					status.Text = "loaded from file"
					status.Importance = widget.MediumImportance
					status.Refresh()
				})
			}()
		})
	})

	loadAgent := widget.NewButton("Refresh keys from agent", func() {
		go func() {
			list, err := agent.ListKeysFromSocket(st.socketPath)
			fyne.Do(func() {
				if err != nil {
					st.exportAgentRows = nil
					st.exportAgentLK.Refresh()
					status.Text = err.Error()
					status.Importance = widget.DangerImportance
					status.Refresh()
					return
				}
				rows := make([]keyRow, len(list))
				for i, k := range list {
					fp := ssh.FingerprintSHA256(k)
					rows[i] = keyRow{fp: fp, display: k.Type() + "  " + fp + "  " + k.Comment, raw: k}
				}
				st.exportAgentRows = rows
				st.exportAgentLK.Refresh()
				status.Text = fmt.Sprintf("%d key(s) from agent", len(rows))
				status.Importance = widget.MediumImportance
				status.Refresh()
			})
		}()
	})

	copyBtn := widget.NewButton("Copy public key to clipboard", func() {
		txt := strings.TrimSpace(st.exportPub.Text)
		if txt == "" {
			status.Text = "nothing to copy"
			status.Importance = widget.DangerImportance
			status.Refresh()
			return
		}
		st.win.Clipboard().SetContent(txt)
		status.Text = "copied"
		status.Importance = widget.SuccessImportance
		status.Refresh()
	})

	saveFile := widget.NewButton("Save public key as…", func() {
		txt := strings.TrimSpace(st.exportPub.Text)
		if txt == "" {
			status.Text = "nothing to save"
			status.Importance = widget.DangerImportance
			status.Refresh()
			return
		}
		st.showFileSave("id_rsa.pub", func(writer fyne.URIWriteCloser, err error) {
			if err != nil {
				dialog.ShowError(err, st.win)
				return
			}
			if writer == nil {
				return
			}
			defer writer.Close()
			_, e := writer.Write([]byte(txt + "\n"))
			fyne.Do(func() {
				if e != nil {
					status.Text = e.Error()
					status.Importance = widget.DangerImportance
				} else {
					status.Text = "saved " + writer.URI().Path()
					status.Importance = widget.SuccessImportance
				}
				status.Refresh()
			})
		})
	})

	agentPane := container.NewBorder(
		container.NewVBox(
			sectionHeading("Agent keys"),
			subtleHint("Select a row to preview the authorized_keys line."),
		), nil, nil, nil,
		container.NewScroll(st.exportAgentLK),
	)

	pubScroll := container.NewScroll(st.exportPub)
	right := container.NewVBox(
		sectionHeading("Public line"),
		subtleHint("One line for authorized_keys or hosting UI."),
		pubScroll,
		widget.NewForm(
			widget.NewFormItem("Type", st.exportKeyType),
			widget.NewFormItem("Source", st.exportSource),
		),
		container.NewHBox(copyBtn, saveFile),
	)

	toolbar := container.NewHBox(loadFile, loadAgent)
	main := container.NewHSplit(agentPane, right)
	main.SetOffset(0.45)

	return container.NewBorder(
		container.NewVBox(
			subtleHint("Load from disk or refresh from the sshush agent; then copy or save."),
			toolbar,
			widget.NewSeparator(),
		),
		container.NewVBox(widget.NewSeparator(), status),
		nil, nil,
		main,
	)
}
