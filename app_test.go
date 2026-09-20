package fbiw

import (
	"context"
	"testing"

	"github.com/movsb/fbiw/internal/canvas/cpu"
)

func newDesktopTestApp() *App {
	ctx, cancel := context.WithCancel(context.Background())
	app := &App{
		ctx:     ctx,
		cancel:  cancel,
		unblock: make(chan struct{}, 1),
	}
	app.themeManager = newThemeManager(app)
	app._EventTarget.box = &BaseBox{}
	app.animation = newAnimationClock(app.ctx, app.animationRunnable, app.wakeUp)
	return app
}

func addDesktopTestDocument(app *App, desktop *Desktop) *Document {
	doc := &Document{app: app}
	desktop.add(doc)
	return doc
}

func TestCloseUnmountedOverlay(t *testing.T) {
	app := newDesktopTestApp()
	doc := &Document{app: app}

	doc.Close()

	if doc.app != nil {
		t.Fatal("closed overlay is still attached to the app")
	}
}

func TestCloseBackgroundDocumentDoesNotDispatchDocChange(t *testing.T) {
	app := newDesktopTestApp()
	active := &Desktop{app: app}
	background := &Desktop{app: app}
	app.desktops.PushBack(active)
	app.desktops.PushBack(background)
	addDesktopTestDocument(app, active)
	doc := addDesktopTestDocument(app, background)

	changes := 0
	app.Listen(DocChange, func(*Event) { changes++ })
	doc.Close()

	if changes != 0 {
		t.Fatalf("got %d DocChange events, want none", changes)
	}
}

func TestCloseActiveTopDispatchesNewTop(t *testing.T) {
	app := newDesktopTestApp()
	desktop := &Desktop{app: app}
	app.desktops.PushFront(desktop)
	want := addDesktopTestDocument(app, desktop)
	top := addDesktopTestDocument(app, desktop)

	var got *Document
	app.Listen(DocChange, func(event *Event) { got = event.DocChange.Doc })
	top.Close()

	if got != want {
		t.Fatalf("active document = %p, want %p", got, want)
	}
}

func TestDesktopAllIsSnapshot(t *testing.T) {
	desktop := &Desktop{}
	docs := []*Document{{}, {}, {}}
	for _, doc := range docs {
		desktop.add(doc)
	}

	all := desktop.All()
	var got []*Document
	for doc := range all {
		got = append(got, doc)
		desktop.remove(doc)
	}

	if len(got) != len(docs) {
		t.Fatalf("iterated over %d documents, want %d", len(got), len(docs))
	}
	for i := range docs {
		if got[i] != docs[i] {
			t.Fatalf("document %d = %p, want %p", i, got[i], docs[i])
		}
	}
}

func TestAppResizeInvalidatesAllDocuments(t *testing.T) {
	app := NewApp(WithRenderer(cpu.New(100, 80)))
	defer app.Close()

	desktop := &Desktop{app: app}
	doc := _NewDocument(100, 80, nil, app.fonts, app.images)
	doc.bindApp(app)
	desktop.add(doc)
	app.desktops.PushFront(desktop)

	overlay := _NewDocument(100, 80, nil, app.fonts, app.images)
	overlay.bindApp(app)
	app.overlay = overlay

	app.resize(160, 90)

	if app.canvas.width != 160 || app.canvas.height != 90 {
		t.Fatalf("canvas size = %dx%d, want 160x90", app.canvas.width, app.canvas.height)
	}
	for name, current := range map[string]*Document{"document": doc, "overlay": overlay} {
		if current.width != 160 || current.height != 90 {
			t.Fatalf("%s size = %dx%d, want 160x90", name, current.width, current.height)
		}
		if !current.layoutDirty || !current.paintDirty {
			t.Fatalf("%s was not invalidated", name)
		}
	}
	if !app.overlayChanged || !app.dirty {
		t.Fatal("resize did not invalidate application overlay/layout")
	}
}
