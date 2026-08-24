package fbiw

import (
	"context"
	"testing"
)

func newDesktopTestApp() *App {
	ctx, cancel := context.WithCancel(context.Background())
	app := &App{
		ctx:     ctx,
		cancel:  cancel,
		unblock: make(chan struct{}, 1),
	}
	app._EventTarget.box = &BaseBox{}
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
