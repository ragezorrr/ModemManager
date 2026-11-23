package main

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

type InteractiveTile struct {
	widget.BaseWidget
	bg       *canvas.Rectangle
	content  fyne.CanvasObject
	onTap    func()
	hovered  bool
	isFilled bool
	appRef   fyne.App
}

func NewInteractiveTile(appRef fyne.App, content fyne.CanvasObject, isFilled bool, onTap func()) *InteractiveTile {
	t := &InteractiveTile{
		bg:       canvas.NewRectangle(color.Transparent),
		content:  content,
		isFilled: isFilled,
		onTap:    onTap,
		appRef:   appRef,
	}
	t.bg.StrokeWidth = 1.0
	t.bg.CornerRadius = 4

	t.updateColors()
	t.ExtendBaseWidget(t)
	return t
}

func (t *InteractiveTile) getVariant() fyne.ThemeVariant {
	if t.appRef != nil {
		return t.appRef.Settings().ThemeVariant()
	}
	return theme.VariantDark
}

// updateColors использует СТРОГО нативные цвета системной темы Fyne!
func (t *InteractiveTile) updateColors() {
	variant := t.getVariant()
	currentTheme := theme.DefaultTheme()
	if t.appRef != nil {
		currentTheme = t.appRef.Settings().Theme()
	}

	if t.hovered {
		// При наведении - акцентный цвет темы (родной цвет подсветки Fyne)
		t.bg.FillColor = currentTheme.Color(theme.ColorNameHover, variant)
		t.bg.StrokeColor = currentTheme.Color(theme.ColorNamePrimary, variant)
		t.bg.StrokeWidth = 1.5
		return
	}

	t.bg.StrokeWidth = 1.0

	if t.isFilled {
		// Активный слот с модемом: цвет кнопки/карточки Fyne
		t.bg.FillColor = currentTheme.Color(theme.ColorNameButton, variant)
		t.bg.StrokeColor = currentTheme.Color(theme.ColorNamePrimary, variant) // деликатная акцентная рамка
	} else {
		// Пустой слот: нативный фон полей ввода Fyne (идеально сливается с общим стилем)
		t.bg.FillColor = currentTheme.Color(theme.ColorNameInputBackground, variant)
		t.bg.StrokeColor = currentTheme.Color(theme.ColorNameSeparator, variant)
	}
}

func (t *InteractiveTile) CreateRenderer() fyne.WidgetRenderer {
	stack := container.NewStack(t.bg, t.content)
	return widget.NewSimpleRenderer(stack)
}

func (t *InteractiveTile) Tapped(_ *fyne.PointEvent) {
	if t.onTap != nil {
		t.onTap()
	}
}

func (t *InteractiveTile) MouseIn(_ *desktop.MouseEvent) {
	t.hovered = true
	t.updateColors()
	t.bg.Refresh()
}

func (t *InteractiveTile) MouseOut() {
	t.hovered = false
	t.updateColors()
	t.bg.Refresh()
}

func (t *InteractiveTile) MouseMoved(_ *desktop.MouseEvent) {}
