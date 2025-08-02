// Copyright (c) 2025 Lee Jones
// Licensed under the MIT License. See LICENSE file in the project root for details.

package main

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework ApplicationServices -framework AppKit
#import <ApplicationServices/ApplicationServices.h>
#import <AppKit/AppKit.h>
#include <stdlib.h>
#include <stdio.h>
#include <unistd.h>

// ---------- Logging ----------
static int g_debug = 0;
static void set_debug(int d) { g_debug = d; }
#define DBG(fmt, ...) do { if (g_debug) fprintf(stderr, "ttyhop: " fmt "\n", ##__VA_ARGS__); } while(0)

// ---- cgo bridges to Go helpers ----
extern void goTmuxSelectEdge(int east, int waitMs);
extern int  goTmuxTryPaneMove(int east);

// ---------- Accessibility trust ----------
static int ensure_trusted_i(void) {
  const void* keys[] = { kAXTrustedCheckOptionPrompt };
  const void* vals[] = { kCFBooleanTrue };
  CFDictionaryRef opts = CFDictionaryCreate(NULL, keys, vals, 1,
                    &kCFTypeDictionaryKeyCallBacks, &kCFTypeDictionaryValueCallBacks);
  Boolean ok = AXIsProcessTrustedWithOptions(opts);
  if (opts) CFRelease(opts);
  DBG("accessibility trusted=%s", ok ? "true" : "false");
  if (ok) return 1; else return 0;
}

// ---------- Frontmost app via NSWorkspace (preferred) ----------
static AXUIElementRef ax_frontmost_app_retained_ws(void) {
  NSRunningApplication *ra = [[NSWorkspace sharedWorkspace] frontmostApplication];
  if (!ra) { DBG("frontmostApplication: nil"); return NULL; }
  pid_t pid = ra.processIdentifier;
  const char *bid = ra.bundleIdentifier ? ra.bundleIdentifier.UTF8String : "";
  const char *name = ra.localizedName ? ra.localizedName.UTF8String : "";
  DBG("frontmost (WS): bid=%s name=%s pid=%d", bid, name, pid);
  return AXUIElementCreateApplication(pid); // retained
}

static void front_app_info_ws(char **outBid, char **outName) {
  *outBid = NULL; *outName = NULL;
  NSRunningApplication *ra = [[NSWorkspace sharedWorkspace] frontmostApplication];
  if (!ra) return;
  if (ra.bundleIdentifier) *outBid = strdup(ra.bundleIdentifier.UTF8String);
  if (ra.localizedName)    *outName = strdup(ra.localizedName.UTF8String);
}

// ---------- Legacy AX focused app (fallback) ----------
static AXUIElementRef ax_focused_app_retained(void) {
  AXUIElementRef sys = AXUIElementCreateSystemWide();
  if (!sys) return NULL;
  CFTypeRef app = NULL;
  AXError e = AXUIElementCopyAttributeValue(sys, kAXFocusedApplicationAttribute, &app);
  CFRelease(sys);
  if (e != kAXErrorSuccess || !app) { DBG("AX focused app: none (err=%d)", e); return NULL; }
  pid_t pid = 0; AXUIElementGetPid((AXUIElementRef)app, &pid);
  NSRunningApplication *ra = [NSRunningApplication runningApplicationWithProcessIdentifier:pid];
  const char *bid = ra.bundleIdentifier ? ra.bundleIdentifier.UTF8String : "";
  const char *name = ra.localizedName ? ra.localizedName.UTF8String : "";
  DBG("frontmost (AX): bid=%s name=%s pid=%d", bid, name, pid);
  return (AXUIElementRef)app; // retained
}

static void front_app_info_ax(char **outBid, char **outName) {
  *outBid = NULL; *outName = NULL;
  AXUIElementRef axApp = ax_focused_app_retained();
  if (!axApp) return;
  pid_t pid = 0; AXUIElementGetPid(axApp, &pid);
  NSRunningApplication *ra = [NSRunningApplication runningApplicationWithProcessIdentifier:pid];
  if (ra.bundleIdentifier) *outBid = strdup(ra.bundleIdentifier.UTF8String);
  if (ra.localizedName)    *outName = strdup(ra.localizedName.UTF8String);
  CFRelease(axApp);
}

// ---------- Helpers ----------
static BOOL ax_get_rect(AXUIElementRef win, CGRect *out) {
  if (!win) return NO;
  CFTypeRef posVal = NULL, sizeVal = NULL;
  if (AXUIElementCopyAttributeValue(win, kAXPositionAttribute, &posVal) != kAXErrorSuccess) return NO;
  if (AXUIElementCopyAttributeValue(win, kAXSizeAttribute, &sizeVal) != kAXErrorSuccess) { if (posVal) CFRelease(posVal); return NO; }
  CGPoint p = CGPointZero; CGSize s = CGSizeZero;
  AXValueGetValue((AXValueRef)posVal, kAXValueCGPointType, &p);
  AXValueGetValue((AXValueRef)sizeVal, kAXValueCGSizeType, &s);
  if (posVal) CFRelease(posVal);
  if (sizeVal) CFRelease(sizeVal);
  *out = (CGRect){p, s};
  return YES;
}

static BOOL app_is_alacritty(AXUIElementRef axApp) {
  if (!axApp) return NO;
  pid_t pid = 0; AXUIElementGetPid(axApp, &pid);
  NSRunningApplication *ra = [NSRunningApplication runningApplicationWithProcessIdentifier:pid];
  if (!ra) return NO;
  NSString *bid = ra.bundleIdentifier ?: @"";
  NSString *name = ra.localizedName ?: @"";
  BOOL ok = [bid isEqualToString:@"org.alacritty"] || [bid isEqualToString:@"io.alacritty"] || [name isEqualToString:@"Alacritty"];
  const char *cbid = bid.UTF8String;
  const char *cname = name.UTF8String;
  DBG("app_is_alacritty=%s (bid=%s name=%s)", ok ? "true" : "false", cbid, cname);
  return ok;
}

static AXUIElementRef app_focused_window(AXUIElementRef axApp) {
  if (!axApp) return NULL;
  CFTypeRef fw = NULL;
  if (AXUIElementCopyAttributeValue(axApp, kAXFocusedWindowAttribute, &fw) == kAXErrorSuccess && fw) {
    return (AXUIElementRef)fw; // retained
  }
  CFTypeRef arr = NULL;
  if (AXUIElementCopyAttributeValue(axApp, kAXWindowsAttribute, &arr) != kAXErrorSuccess || !arr) return NULL;
  CFArrayRef wins = (CFArrayRef)arr;
  AXUIElementRef w = NULL;
  if (CFArrayGetCount(wins) > 0) {
    w = (AXUIElementRef)CFRetain(CFArrayGetValueAtIndex(wins, 0));
  }
  CFRelease(arr);
  return w;
}

static CFArrayRef app_windows_retained(AXUIElementRef axApp) {
  if (!axApp) return NULL;
  CFTypeRef arr = NULL;
  if (AXUIElementCopyAttributeValue(axApp, kAXWindowsAttribute, &arr) != kAXErrorSuccess || !arr) return NULL;
  return (CFArrayRef)arr; // retained
}

static void focus_window(AXUIElementRef axApp, AXUIElementRef win) {
  if (!axApp || !win) return;
  AXUIElementPerformAction(win, kAXRaiseAction);
  AXUIElementSetAttributeValue(win, kAXMainAttribute, kCFBooleanTrue);
  AXUIElementSetAttributeValue(win, kAXFocusedAttribute, kCFBooleanTrue);
  AXUIElementSetAttributeValue(axApp, kAXFocusedWindowAttribute, win);
  pid_t pid = 0; AXUIElementGetPid(win, &pid);
  NSRunningApplication *ra = [NSRunningApplication runningApplicationWithProcessIdentifier:pid];
  if (ra) [ra activateWithOptions:0];
}

// ---- Send C-<key> to a process (for tmux binds) ----
// US layout keycodes: 'h' = 4, 'l' = 37.
static void send_ctrl_key_to_pid(pid_t pid, CGKeyCode key, int times, useconds_t gap_us) {
  for (int i=0; i<times; i++) {
    CGEventRef down = CGEventCreateKeyboardEvent(NULL, key, true);
    CGEventRef up   = CGEventCreateKeyboardEvent(NULL, key, false);
    CGEventSetFlags(down, kCGEventFlagMaskControl);
    CGEventSetFlags(up,   kCGEventFlagMaskControl);
    CGEventPostToPid(pid, down);
    CGEventPostToPid(pid, up);
    CFRelease(down); CFRelease(up);
    if (gap_us) usleep(gap_us);
  }
}

// Returns 0 on success. Non-zero = "no move".
static int alacritty_focus_neighbor_dbg(int east, int debug, int do_edge, int edge_steps, int wait_ms) {
  g_debug = debug;

  // NEW: try tmux pane move first (no AX needed).
  if (goTmuxTryPaneMove(east)) {
    DBG("tmux: moved pane %s", east ? "right" : "left");
    return 0;
  }

  if (!ensure_trusted_i()) { DBG("denied: accessibility not trusted"); return 20; }

  AXUIElementRef axApp = ax_frontmost_app_retained_ws();
  if (!axApp) axApp = ax_focused_app_retained();
  if (!axApp) { DBG("denied: could not obtain front app"); return 10; }

  if (!app_is_alacritty(axApp)) { CFRelease(axApp); DBG("denied: front app is not Alacritty"); return 1; }

  AXUIElementRef meWin = app_focused_window(axApp);
  if (!meWin) { CFRelease(axApp); DBG("denied: no focused window"); return 2; }

  CGRect meR; if (!ax_get_rect(meWin, &meR)) { CFRelease(meWin); CFRelease(axApp); DBG("denied: cannot read current window rect"); return 3; }
  CGFloat cx = CGRectGetMidX(meR), cy = CGRectGetMidY(meR), h = CGRectGetHeight(meR);

  CFArrayRef wins = app_windows_retained(axApp);
  if (!wins) { CFRelease(meWin); CFRelease(axApp); DBG("denied: cannot list windows"); return 4; }

  CFIndex n = CFArrayGetCount(wins);
  DBG("windows in app: %ld", (long)n);

  AXUIElementRef best = NULL; CGFloat bestDx = CGFLOAT_MAX; int bestIdx = -1;

  for (CFIndex i = 0; i < n; i++) {
    AXUIElementRef w = (AXUIElementRef)CFArrayGetValueAtIndex(wins, i);
    if (CFEqual(w, meWin)) continue;
    CGRect r; if (!ax_get_rect(w, &r)) continue;
    CGFloat dx = CGRectGetMidX(r) - cx;
    CGFloat dy = fabs(CGRectGetMidY(r) - cy);
    int horiz = (dy <= h * 0.75);
    if (g_debug) {
      fprintf(stderr, "ttyhop: cand[%ld] mid=(%.1f,%.1f) dx=%.1f dy=%.1f horiz=%d\n",
              (long)i, CGRectGetMidX(r), CGRectGetMidY(r), dx, dy, horiz);
    }
    if (!horiz) continue;
    if (!east && dx < 0 && -dx < bestDx) { bestDx = -dx; best = w; bestIdx = (int)i; }
    if ( east && dx > 0 &&  dx < bestDx) { bestDx =  dx; best = w; bestIdx = (int)i; }
  }

  int rc = 5;
  if (best) {
    const char *dirstr;
    if (east) dirstr = "east"; else dirstr = "west";
    DBG("focusing neighbor %s: idx=%d, distance=%.1f", dirstr, bestIdx, bestDx);
    focus_window(axApp, best);

    if (do_edge) {
      // Prefer tmux IPC to land on edge pane in the destination window.
      goTmuxSelectEdge(east, wait_ms);
      DBG("edge-nudge: tmux IPC select edge");
      // Keystroke-based nudge is still available (kept for reference):
      // usleep(20000);
      // pid_t pid = 0; AXUIElementGetPid(best, &pid);
      // if (east) { send_ctrl_key_to_pid(pid, 4, edge_steps, 2000); DBG("edge-nudge: C-h x%d", edge_steps); }
      // else      { send_ctrl_key_to_pid(pid, 37, edge_steps, 2000); DBG("edge-nudge: C-l x%d", edge_steps); }
    }
    rc = 0;
  } else {
    const char *dirstr2;
    if (east) dirstr2 = "east"; else dirstr2 = "west";
    DBG("no neighbor %s found", dirstr2);
  }

  CFRelease(wins);
  CFRelease(meWin);
  CFRelease(axApp);
  return rc;
}

*/
import "C"

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
	"unsafe"
)

const (
	defaultWaitMs  = 200
	pollIntervalMs = 25
)

// Hopper provides an interface for all platform-specific (Cgo) interactions.
type Hopper interface {
	FocusNeighbor(east bool, debug bool, doEdge bool, edgeSteps int) int
	SetDebug(debug bool)
	SetWaitMs(waitMs int)
	IsTrusted() bool
	GetFrontAppInfo() (bid, name string, source string)
}

// cgoHopper is the unexported, production implementation of Hopper that calls Cgo functions.
type cgoHopper struct {
	waitMs int
}

// newHopper creates a new production Hopper that uses Cgo.
func newHopper() Hopper {
	return &cgoHopper{}
}

func (h *cgoHopper) FocusNeighbor(east bool, debug bool, doEdge bool, edgeSteps int) int {
	cEast := C.int(0)
	if east {
		cEast = 1
	}
	cDebug := C.int(0)
	if debug {
		cDebug = 1
	}
	cDoEdge := C.int(1)
	if !doEdge {
		cDoEdge = 0
	}
	cEdgeSteps := C.int(edgeSteps)
	cWaitMs := C.int(h.waitMs)
	return int(C.alacritty_focus_neighbor_dbg(cEast, cDebug, cDoEdge, cEdgeSteps, cWaitMs))
}

func (h *cgoHopper) SetDebug(debug bool) {
	cDebug := C.int(0)
	if debug {
		cDebug = 1
	}
	C.set_debug(cDebug)
}

func (h *cgoHopper) SetWaitMs(waitMs int) {
	h.waitMs = waitMs
}

func (h *cgoHopper) IsTrusted() bool {
	return C.ensure_trusted_i() == 1
}

func (h *cgoHopper) GetFrontAppInfo() (string, string, string) {
	var bidWS, nameWS *C.char
	C.front_app_info_ws(&bidWS, &nameWS)
	if bidWS != nil {
		defer C.free(unsafe.Pointer(bidWS))
	}
	if nameWS != nil {
		defer C.free(unsafe.Pointer(nameWS))
	}
	bid := C.GoString(bidWS)
	name := C.GoString(nameWS)

	if bid == "" && name == "" {
		var bidAX, nameAX *C.char
		C.front_app_info_ax(&bidAX, &nameAX)
		if bidAX != nil {
			defer C.free(unsafe.Pointer(bidAX))
		}
		if nameAX != nil {
			defer C.free(unsafe.Pointer(nameAX))
		}
		return C.GoString(bidAX), C.GoString(nameAX), "AX"
	}
	return bid, name, "WS"
}

// ---------- small stderr logger to match C DBG prefix ----------
func dbg(format string, a ...any) {
	if os.Getenv("TTYHOP_LOG") == "1" {
		fmt.Fprintf(os.Stderr, "ttyhop: "+format+"\n", a...)
	}
}

// ---------- tmux IPC helpers ----------

type runTmuxCmdFunc func(args ...string) (string, error)

var runTmuxCmd runTmuxCmdFunc = defaultRunTmuxCmd

func defaultRunTmuxCmd(args ...string) (string, error) {
	cmd := exec.Command("tmux", args...)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out.String()), nil
}

// Try to move tmux pane first; return true if moved.
// NOTE: #{pane_at_left/right} == 1 means you are AT the outer edge (no neighbor that way).
func tmuxTryPaneMove(east bool) bool {
	if os.Getenv("TMUX") == "" {
		return false
	}

	// Active pane before move
	oldID, err := runTmuxCmd("display", "-p", "#{pane_id}")
	if err != nil || strings.TrimSpace(oldID) == "" {
		return false
	}

	// Edge check (1 = at edge, no neighbor; 0 = has neighbor)
	edgeKey := "#{pane_at_right}"
	dir := "-R"
	if !east {
		edgeKey = "#{pane_at_left}"
		dir = "-L"
	}
	edge, err := runTmuxCmd("display", "-p", edgeKey)
	if err != nil {
		return false
	}
	if strings.TrimSpace(edge) == "1" {
		return false // at edge, let caller hop windows
	}

	// Move relative to the active pane (no -t)
	_, _ = runTmuxCmd("select-pane", dir)

	// Verify it actually changed pane
	newID, _ := runTmuxCmd("display", "-p", "#{pane_id}")
	if strings.TrimSpace(newID) == "" || newID == oldID {
		return false
	}

	dbg("tmux: pane move %s via IPC", map[bool]string{true: "right", false: "left"}[east])
	return true
}

//export goTmuxTryPaneMove
func goTmuxTryPaneMove(east C.int) C.int {
	if tmuxTryPaneMove(east != 0) {
		return 1
	}
	return 0
}

// Select the appropriate edge pane in the newly focused Alacritty window.
// Uses the newly active tmux client; #{pane_at_left/right} (1 = outer edge).
func pickActiveClient() (string, error) {
	out, err := runTmuxCmd("list-clients", "-F", "#{client_tty} #{client_active} #{client_activity}")
	if err != nil || out == "" {
		return "", err
	}
	type rec struct {
		tty    string
		active bool
		act    int64
	}
	var best rec
	var have bool
	for _, ln := range strings.Split(out, "\n") {
		f := strings.Fields(ln)
		if len(f) < 3 {
			continue
		}
		a, _ := strconv.ParseInt(f[2], 10, 64)
		r := rec{tty: f[0], active: (f[1] == "1"), act: a}
		if r.active {
			return r.tty, nil
		}
		if !have || r.act > best.act {
			best, have = r, true
		}
	}
	if have {
		return best.tty, nil
	}
	return "", nil
}

func tmuxSelectEdgePane(east bool, waitMs int) {
	// Wait briefly for the newly focused Alacritty window's tmux client to become active.
	if waitMs <= 0 {
		waitMsStr := os.Getenv("TTYHOP_EDGE_WAIT_MS")
		waitMsVal, err := strconv.Atoi(waitMsStr)
		if err != nil || waitMsVal <= 0 {
			waitMs = defaultWaitMs // default
		} else {
			waitMs = waitMsVal
		}
	}
	dbg("using edge wait: %dms", waitMs)

	pollInterval := pollIntervalMs * time.Millisecond
	numPolls := waitMs / pollIntervalMs

	for i := 0; i < numPolls; i++ {
		time.Sleep(pollInterval)

		tty, err := pickActiveClient()
		if err != nil || strings.TrimSpace(tty) == "" {
			continue
		}

		// Query that client's current window
		win, err := runTmuxCmd("display", "-p", "-t", tty, "#{window_id}")
		if err != nil || strings.TrimSpace(win) == "" {
			continue
		}

		// List panes in that window; use pane_at_left/right (1 = outer edge)
		panes, err := runTmuxCmd("list-panes", "-t", win, "-F", "#{pane_id} #{pane_at_left} #{pane_at_right}")
		if err != nil || strings.TrimSpace(panes) == "" {
			continue
		}

		var target string
		for _, ln := range strings.Split(panes, "\n") {
			f := strings.Fields(ln)
			if len(f) != 3 {
				continue
			}
			id, atL, atR := f[0], f[1], f[2]
			if east && atL == "1" { // moving right -> land on LEFTMOST pane
				target = id
				break
			}
			if !east && atR == "1" { // moving left -> land on RIGHTMOST pane
				target = id
				break
			}
		}
		if target != "" {
			_, _ = runTmuxCmd("select-pane", "-t", target)
			dbg("tmux: landed on edge pane %s", map[bool]string{true: "LEFTMOST", false: "RIGHTMOST"}[east])
		}
		return
	}
}

//export goTmuxSelectEdge
func goTmuxSelectEdge(east C.int, waitMs C.int) {
	tmuxSelectEdgePane(east != 0, int(waitMs))
}
