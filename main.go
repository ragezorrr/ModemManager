package main

import (
	"fmt"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"e173-control/pkg/manager"
	"e173-control/pkg/modem"
)

type AppUI struct {
	myApp       fyne.App
	win         fyne.Window
	pool        *manager.Pool
	devices     []modem.Info
	mu          sync.Mutex
	selectedIdx int

	// Главная (Dashboard)
	totalCard   *widget.Card
	readyCard   *widget.Card
	noSimCard   *widget.Card
	dashSummary *widget.Label

	// Модемы
	modemsContainer *fyne.Container
	isGridView      bool
	table           *widget.Table
	gridContainer   *fyne.Container
	statusLabel     *widget.Label
	btnViewToggle   *widget.Button

	// Управление
	modemSelect *widget.Select
	phoneEntry  *widget.Entry
	smsEntry    *widget.Entry
	smsStatus   *widget.Label
	ussdEntry   *widget.Entry
	ussdResult  *widget.Entry
	atEntry     *widget.Entry
	atOutput    *widget.Entry

	// Лог
	logOutput *widget.Entry
	logMutex  sync.Mutex

	// Настройки
	apiServer    *manager.APIServer
	apiCheck     *widget.Check
	apiStatus    *widget.Label
	baudSelect   *widget.Select
	pollInterval *widget.Select
	themeSelect  *widget.RadioGroup
}

var columns = []string{"#", "Порт", "IMEI", "Модель", "Номер SIM", "SIM", "Оператор", "Сигнал", "Баланс / USSD", "Статус"}
var colWidths = []float32{35, 110, 150, 80, 130, 110, 110, 100, 200, 110}

func main() {
	// Защита от одновременного запуска нескольких копий программы
	lockListener, err := net.Listen("tcp", "127.0.0.1:18081")
	if err != nil {
		fmt.Println("Внимание: Программа e173-control уже запущена в другом окне!")
		return
	}
	defer lockListener.Close()

	myApp := app.NewWithID("com.e173.multimanager")
	myWindow := myApp.NewWindow("Huawei E173 Multi-Modem Control Suite")
	myWindow.Resize(fyne.NewSize(1150, 750))
	myWindow.SetIcon(theme.ComputerIcon())

	// Перехватываем нажатие на крестик: сворачиваем в трей вместо закрытия
	myWindow.SetCloseIntercept(func() {
		myWindow.Hide()
	})

	ui := &AppUI{
		myApp:       myApp,
		win:         myWindow,
		devices:     make([]modem.Info, 0),
		selectedIdx: -1,
		isGridView:  false,
	}

	// Настройка меню системного трея (Fyne сам автоматически добавляет пункт "Выйти")
	if desk, ok := myApp.(desktop.App); ok {
		trayMenu := fyne.NewMenu("E173 Manager",
			fyne.NewMenuItem("Показать окно", func() {
				myWindow.Show()
			}),
		)
		desk.SetSystemTrayMenu(trayMenu)
		desk.SetSystemTrayIcon(theme.ComputerIcon())
	}

	ui.pool = manager.NewPool(func(devs []modem.Info) {
		ui.mu.Lock()
		ui.devices = devs
		ui.mu.Unlock()

		ui.updateUIState()
	})

	// Сначала инициализируем API сервер ДО построения вкладок интерфейса (порт 5001 как в расширении Binotel)
	ui.apiServer = manager.NewAPIServer(ui.pool, 5001)

	mainTabs := container.NewAppTabs(
		container.NewTabItemWithIcon("Главная", theme.HomeIcon(), ui.buildDashboardTab()),
		container.NewTabItemWithIcon("Модемы", theme.ListIcon(), ui.buildModemsTab()),
		container.NewTabItemWithIcon("Управление", theme.MailSendIcon(), ui.buildControlTab()),
		container.NewTabItemWithIcon("Лог событий", theme.DocumentIcon(), ui.buildLogTab()),
		container.NewTabItemWithIcon("Настройки", theme.SettingsIcon(), ui.buildSettingsTab()),
	)
	mainTabs.SetTabLocation(container.TabLocationTop)

	myWindow.SetContent(mainTabs)

	ui.pool.Start()
	_ = ui.apiServer.Start()

	defer func() {
		if ui.apiServer != nil {
			ui.apiServer.Stop()
		}
		ui.pool.Stop()
	}()

	ui.appendLog("Система запущена. API сервер активен (порт 5001).")

	myWindow.ShowAndRun()
}

func (ui *AppUI) appendLog(msg string) {
	ui.logMutex.Lock()
	defer ui.logMutex.Unlock()

	timestamp := time.Now().Format("15:04:05")
	entry := fmt.Sprintf("[%s] %s\n", timestamp, msg)

	if ui.logOutput != nil {
		ui.logOutput.SetText(ui.logOutput.Text + entry)
		ui.logOutput.CursorRow = len(strings.Split(ui.logOutput.Text, "\n"))
	}
}

func (ui *AppUI) updateUIState() {
	ui.mu.Lock()
	devs := make([]modem.Info, len(ui.devices))
	copy(devs, ui.devices)
	ui.mu.Unlock()

	total := len(devs)
	ready := 0
	noSim := 0
	options := make([]string, 0, total)

	for i, d := range devs {
		if d.SimState == modem.SimReady {
			ready++
		} else {
			noSim++
		}
		label := fmt.Sprintf("#%d: %s (%s)", i+1, d.Port, d.IMEI)
		options = append(options, label)
	}

	if ui.totalCard != nil {
		ui.totalCard.SetSubTitle(fmt.Sprintf("%d шт.", total))
	}
	if ui.readyCard != nil {
		ui.readyCard.SetSubTitle(fmt.Sprintf("%d шт.", ready))
	}
	if ui.noSimCard != nil {
		ui.noSimCard.SetSubTitle(fmt.Sprintf("%d шт.", noSim))
	}
	if ui.dashSummary != nil {
		ui.dashSummary.SetText(fmt.Sprintf("Активных модемов: %d | С сетью: %d | Без SIM: %d | Последнее обновление: %s",
			total, ready, noSim, time.Now().Format("15:04:05")))
	}
	if ui.statusLabel != nil {
		ui.statusLabel.SetText(fmt.Sprintf("Всего устройств: %d (В сети: %d, Без SIM: %d)", total, ready, noSim))
	}

	if ui.isGridView {
		ui.refreshGrid()
	} else if ui.table != nil {
		ui.table.Refresh()
	}

	if ui.modemSelect != nil {
		oldSelected := ui.modemSelect.Selected
		ui.modemSelect.Options = options
		if len(options) > 0 {
			if oldSelected == "" {
				ui.modemSelect.SetSelected(options[0])
			}
		}
		ui.modemSelect.Refresh()
	}
}

// -------------------------------------------------------------
// 1. Главная страница (Dashboard) - только статус и карточки
// -------------------------------------------------------------
func (ui *AppUI) buildDashboardTab() fyne.CanvasObject {
	ui.totalCard = widget.NewCard("Всего модемов", "0 шт.", widget.NewLabel("Подключенные USB-порты"))
	ui.readyCard = widget.NewCard("Готовы к работе", "0 шт.", widget.NewLabel("SIM активна, есть сеть"))
	ui.noSimCard = widget.NewCard("Требуют внимания", "0 шт.", widget.NewLabel("Нет SIM или ошибка"))

	apiCard := widget.NewCard("Chrome API (5001)", "Активен 🟢", widget.NewLabel("Связка с панелью Binotel"))

	cardsRow := container.NewGridWithColumns(4, ui.totalCard, ui.readyCard, ui.noSimCard, apiCard)

	ui.dashSummary = widget.NewLabel("Инициализация пула модемов...")
	ui.dashSummary.TextStyle = fyne.TextStyle{Italic: true}

	btnRescan := widget.NewButtonWithIcon("Пересканировать порты сейчас", theme.ViewRefreshIcon(), func() {
		ui.appendLog("Запуск пересканирования USB...")
		ui.pool.Rescan()
	})

	topBox := container.NewVBox(
		cardsRow,
		container.NewHBox(ui.dashSummary, widget.NewSeparator(), btnRescan),
		widget.NewSeparator(),
	)

	return container.NewBorder(topBox, nil, nil, nil, widget.NewLabel(""))
}

// -------------------------------------------------------------
// 2. Вкладка "Модемы" (Таблица / Сетка 7х7 без скролла)
// -------------------------------------------------------------
func (ui *AppUI) buildModemsTab() fyne.CanvasObject {
	ui.statusLabel = widget.NewLabel("Поиск модемов...")
	ui.statusLabel.TextStyle = fyne.TextStyle{Bold: true}

	btnRefresh := widget.NewButtonWithIcon("Обновить", theme.ViewRefreshIcon(), func() {
		ui.pool.Rescan()
	})

	ui.btnViewToggle = widget.NewButtonWithIcon("Вид: Плитка 7х7", theme.GridIcon(), func() {
		ui.isGridView = !ui.isGridView
		if ui.isGridView {
			ui.btnViewToggle.SetText("Вид: Таблица")
			ui.btnViewToggle.SetIcon(theme.ListIcon())
			ui.showGridView()
		} else {
			ui.btnViewToggle.SetText("Вид: Плитка 7х7")
			ui.btnViewToggle.SetIcon(theme.GridIcon())
			ui.showTableView()
		}
	})

	topBar := container.NewBorder(nil, nil, ui.statusLabel, container.NewHBox(ui.btnViewToggle, btnRefresh))

	ui.table = ui.createTableWidget()
	ui.gridContainer = container.NewMax()
	ui.modemsContainer = container.NewMax(ui.table)

	return container.NewBorder(topBar, nil, nil, nil, ui.modemsContainer)
}

func (ui *AppUI) showTableView() {
	ui.modemsContainer.Objects = []fyne.CanvasObject{ui.table}
	ui.table.Refresh()
	ui.modemsContainer.Refresh()
}

func (ui *AppUI) showGridView() {
	ui.refreshGrid()
	ui.modemsContainer.Objects = []fyne.CanvasObject{ui.gridContainer}
	ui.modemsContainer.Refresh()
}

// refreshGrid строит матрицу 7x7 строго по ФИЗИЧЕСКИМ СЛОТАМ (1..49)
func (ui *AppUI) refreshGrid() {
	ui.mu.Lock()
	devs := make([]modem.Info, len(ui.devices))
	copy(devs, ui.devices)
	ui.mu.Unlock()

	// Индексируем модемы строго по их физическому номеру слота (1..49)
	slotMap := make(map[int]*modem.Info)
	for i := range devs {
		s := devs[i].Slot
		if s >= 1 && s <= 49 {
			slotMap[s] = &devs[i]
		}
	}

	rowsContainer := container.NewGridWithRows(7)

	for r := 0; r < 7; r++ {
		row := container.NewGridWithColumns(7)
		for c := 0; c < 7; c++ {
			slotNum := r*7 + c + 1

			if d, ok := slotMap[slotNum]; ok {
				tile := ui.createCompactTile(slotNum, d)
				row.Add(tile)
			} else {
				emptyTile := ui.createCompactEmptyTile(slotNum)
				row.Add(emptyTile)
			}
		}
		rowsContainer.Add(row)
	}

	ui.gridContainer.Objects = []fyne.CanvasObject{rowsContainer}
	ui.gridContainer.Refresh()
}

// createCompactTile создает нативную гармоничную плитку в цветах Fyne
func (ui *AppUI) createCompactTile(slot int, d *modem.Info) fyne.CanvasObject {
	currentTheme := theme.DefaultTheme()
	variant := theme.VariantDark
	if ui.myApp != nil {
		currentTheme = ui.myApp.Settings().Theme()
		variant = ui.myApp.Settings().ThemeVariant()
	}

	statusColor := currentTheme.Color(theme.ColorNameError, variant)
	statusText := "Нет SIM"

	if d.SimState == modem.SimReady {
		statusColor = currentTheme.Color(theme.ColorNameSuccess, variant)
		statusText = d.Operator
		if statusText == "" || statusText == "—" {
			statusText = "В сети"
		}
	} else if strings.Contains(string(d.SimState), "Нет") {
		statusColor = currentTheme.Color(theme.ColorNameWarning, variant)
	}

	indicator := canvas.NewCircle(statusColor)
	indicatorWrap := container.NewGridWrap(fyne.NewSize(8, 8), indicator)

	// Номер слота - родной цвет заголовков
	txtSlot := canvas.NewText(fmt.Sprintf("#%d", slot), currentTheme.Color(theme.ColorNameForeground, variant))
	txtSlot.TextStyle = fyne.TextStyle{Bold: true}
	txtSlot.TextSize = 12

	// Имя порта - цвет акцента Fyne
	portShort := strings.TrimPrefix(d.Port, "/dev/")
	txtPort := canvas.NewText(portShort, currentTheme.Color(theme.ColorNamePrimary, variant))
	txtPort.TextSize = 10
	txtPort.Alignment = fyne.TextAlignTrailing

	topRow := container.NewBorder(nil, nil, container.NewHBox(indicatorWrap, txtSlot), nil, txtPort)

	// Основная строка (Оператор) - родной текст
	txtOp := canvas.NewText(statusText, currentTheme.Color(theme.ColorNameForeground, variant))
	txtOp.TextStyle = fyne.TextStyle{Bold: true}
	txtOp.TextSize = 11
	txtOp.Alignment = fyne.TextAlignCenter

	// Вторая строка (Телефон или хвост IMEI)
	subVal := d.PhoneNumber
	if subVal == "" || subVal == "—" {
		if len(d.IMEI) > 4 {
			subVal = ".." + d.IMEI[len(d.IMEI)-4:]
		} else {
			subVal = d.IMEI
		}
	}
	txtSub := canvas.NewText(subVal, currentTheme.Color(theme.ColorNamePlaceHolder, variant))
	txtSub.TextSize = 10
	txtSub.Alignment = fyne.TextAlignCenter

	body := container.NewVBox(
		topRow,
		container.NewCenter(txtOp),
		container.NewCenter(txtSub),
	)

	onTap := func() {
		ui.mu.Lock()
		for idx, dev := range ui.devices {
			if dev.IMEI == d.IMEI {
				ui.selectedIdx = idx
				break
			}
		}
		ui.mu.Unlock()

		if ui.modemSelect != nil {
			label := fmt.Sprintf("#%d: %s (%s)", d.Slot, d.Port, d.IMEI)
			ui.modemSelect.SetSelected(label)
		}

		slotEntry := widget.NewEntry()
		slotEntry.SetText(fmt.Sprintf("%d", d.Slot))

		items := []*widget.FormItem{
			widget.NewFormItem("Порт:", widget.NewLabel(d.Port)),
			widget.NewFormItem("IMEI:", widget.NewLabel(d.IMEI)),
			widget.NewFormItem("Оператор:", widget.NewLabel(d.Operator)),
			widget.NewFormItem("Номер SIM:", widget.NewLabel(d.PhoneNumber)),
			widget.NewFormItem("USB путь:", widget.NewLabel(d.USBPath)),
			widget.NewFormItem("Номер слота в хабе (1-49):", slotEntry),
		}

		dialog.ShowForm("Свойства слота #"+fmt.Sprintf("%d", d.Slot), "Сохранить слот", "Закрыть", items, func(save bool) {
			if save {
				var newSlot int
				if _, err := fmt.Sscanf(slotEntry.Text, "%d", &newSlot); err == nil && newSlot >= 1 && newSlot <= 49 {
					ui.pool.SetSlotForDevice(d.IMEI, newSlot)
				}
			}
		}, ui.win)
	}

	return NewInteractiveTile(ui.myApp, body, true, onTap)
}

// createCompactEmptyTile создает пустой слот в родных цветах Fyne
func (ui *AppUI) createCompactEmptyTile(slot int) fyne.CanvasObject {
	currentTheme := theme.DefaultTheme()
	variant := theme.VariantDark
	if ui.myApp != nil {
		currentTheme = ui.myApp.Settings().Theme()
		variant = ui.myApp.Settings().ThemeVariant()
	}

	// Родной мягкий цвет заполнителя Fyne (ColorNamePlaceHolder)
	txtSlot := canvas.NewText(fmt.Sprintf("#%d", slot), currentTheme.Color(theme.ColorNamePlaceHolder, variant))
	txtSlot.TextStyle = fyne.TextStyle{Bold: true}
	txtSlot.TextSize = 12
	txtSlot.Alignment = fyne.TextAlignCenter

	center := container.NewCenter(txtSlot)

	onTap := func() {
		dialog.ShowInformation("Свободный слот", fmt.Sprintf("Разъем #%d на 49-портовом хабе свободен", slot), ui.win)
	}

	return NewInteractiveTile(ui.myApp, center, false, onTap)
}

func (ui *AppUI) createTableWidget() *widget.Table {
	t := widget.NewTable(
		func() (int, int) {
			ui.mu.Lock()
			defer ui.mu.Unlock()
			return len(ui.devices) + 1, len(columns)
		},
		func() fyne.CanvasObject {
			lbl := widget.NewLabel("template cell data")
			lbl.Truncation = fyne.TextTruncateEllipsis
			return lbl
		},
		func(id widget.TableCellID, obj fyne.CanvasObject) {
			lbl := obj.(*widget.Label)
			if id.Row == 0 {
				lbl.SetText(columns[id.Col])
				lbl.TextStyle = fyne.TextStyle{Bold: true}
				return
			}

			ui.mu.Lock()
			if id.Row-1 >= len(ui.devices) {
				ui.mu.Unlock()
				lbl.SetText("")
				return
			}
			d := ui.devices[id.Row-1]
			ui.mu.Unlock()

			lbl.TextStyle = fyne.TextStyle{Bold: false}

			switch id.Col {
			case 0:
				if d.Slot > 0 {
					lbl.SetText(fmt.Sprintf("#%d", d.Slot))
				} else {
					lbl.SetText(fmt.Sprintf("%d", id.Row))
				}
			case 1:
				lbl.SetText(d.Port)
			case 2:
				lbl.SetText(d.IMEI)
			case 3:
				lbl.SetText(d.Model)
			case 4:
				lbl.SetText(d.PhoneNumber)
			case 5:
				lbl.SetText(string(d.SimState))
			case 6:
				lbl.SetText(d.Operator)
			case 7:
				if d.SignalPercent > 0 {
					lbl.SetText(fmt.Sprintf("%d%% (%d dBm)", d.SignalPercent, d.SignalDbm))
				} else {
					lbl.SetText("—")
				}
			case 8:
				lbl.SetText(d.Balance)
			case 9:
				lbl.SetText(d.Status)
			}
		},
	)

	for i, w := range colWidths {
		t.SetColumnWidth(i, w)
	}

	t.OnSelected = func(id widget.TableCellID) {
		if id.Row > 0 {
			ui.mu.Lock()
			idx := id.Row - 1
			if idx < len(ui.devices) {
				ui.selectedIdx = idx
				target := ui.devices[idx]
				ui.mu.Unlock()

				if ui.modemSelect != nil {
					label := fmt.Sprintf("#%d: %s (%s)", id.Row, target.Port, target.IMEI)
					ui.modemSelect.SetSelected(label)
				}
				return
			}
			ui.mu.Unlock()
		}
	}

	return t
}

// -------------------------------------------------------------
// 3. Вкладка "Управление"
// -------------------------------------------------------------
func (ui *AppUI) getSelectedModemDevice() (*modem.Device, *modem.Info) {
	ui.mu.Lock()
	defer ui.mu.Unlock()

	if len(ui.devices) == 0 {
		return nil, nil
	}

	selectedText := ""
	if ui.modemSelect != nil {
		selectedText = ui.modemSelect.Selected
	}

	for i, d := range ui.devices {
		label := fmt.Sprintf("#%d: %s (%s)", i+1, d.Port, d.IMEI)
		if label == selectedText || (selectedText == "" && ui.selectedIdx >= 0 && ui.devices[ui.selectedIdx].IMEI == d.IMEI) {
			dev := ui.pool.GetDeviceByIMEI(d.IMEI)
			infoCopy := d
			return dev, &infoCopy
		}
	}

	first := ui.devices[0]
	return ui.pool.GetDeviceByIMEI(first.IMEI), &first
}

func (ui *AppUI) buildControlTab() fyne.CanvasObject {
	ui.modemSelect = widget.NewSelect([]string{}, func(s string) {})
	ui.modemSelect.PlaceHolder = "Выберите модем из списка..."

	topSelect := container.NewHBox(
		widget.NewLabelWithStyle("Целевой модем:", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		container.NewGridWrap(fyne.NewSize(380, 36), ui.modemSelect),
	)

	subTabs := container.NewAppTabs(
		container.NewTabItemWithIcon("USSD / Баланс", theme.AccountIcon(), ui.buildUSSDSubTab()),
		container.NewTabItemWithIcon("SMS Сообщения", theme.MailSendIcon(), ui.buildSMSSubTab()),
		container.NewTabItemWithIcon("AT-Терминал", theme.ComputerIcon(), ui.buildTerminalSubTab()),
	)

	return container.NewBorder(
		container.NewVBox(topSelect, widget.NewSeparator()),
		nil, nil, nil,
		subTabs,
	)
}

func (ui *AppUI) buildUSSDSubTab() fyne.CanvasObject {
	ui.ussdEntry = widget.NewEntry()
	ui.ussdEntry.SetText("*100#")
	ui.ussdEntry.SetPlaceHolder("*100#")

	ui.ussdResult = widget.NewMultiLineEntry()
	ui.ussdResult.SetPlaceHolder("Здесь появится ответ оператора на USSD...")
	ui.ussdResult.Wrapping = fyne.TextWrapWord

	btnSend := widget.NewButtonWithIcon("Отправить USSD", theme.MailSendIcon(), func() {
		dev, info := ui.getSelectedModemDevice()
		if dev == nil {
			dialog.ShowInformation("Внимание", "Нет доступных подключенных модемов!", ui.win)
			return
		}

		code := strings.TrimSpace(ui.ussdEntry.Text)
		if code == "" {
			return
		}

		ui.ussdResult.SetText(fmt.Sprintf("[%s] Отправка USSD %s...", info.Port, code))
		ui.appendLog(fmt.Sprintf("[%s] Запрос USSD: %s", info.Port, code))

		go func() {
			resp, err := dev.SendUSSD(code, 15*time.Second)
			if err != nil {
				ui.ussdResult.SetText(fmt.Sprintf("[%s] Ошибка: %v", info.Port, err))
				ui.appendLog(fmt.Sprintf("[%s] Ошибка USSD: %v", info.Port, err))
			} else {
				ui.ussdResult.SetText(fmt.Sprintf("[%s] Ответ оператора:\n\n%s", info.Port, resp))
				ui.appendLog(fmt.Sprintf("[%s] USSD ответ получен", info.Port))
			}
		}()
	})

	return container.NewBorder(
		container.NewHBox(widget.NewLabel("Команда:"), container.NewGridWrap(fyne.NewSize(150, 36), ui.ussdEntry), btnSend),
		nil, nil, nil,
		ui.ussdResult,
	)
}

func (ui *AppUI) buildSMSSubTab() fyne.CanvasObject {
	ui.phoneEntry = widget.NewEntry()
	ui.phoneEntry.SetPlaceHolder("+79001234567")

	ui.smsEntry = widget.NewMultiLineEntry()
	ui.smsEntry.SetPlaceHolder("Текст SMS сообщения...")
	ui.smsEntry.Wrapping = fyne.TextWrapWord

	ui.smsStatus = widget.NewLabel("")

	btnSend := widget.NewButtonWithIcon("Отправить SMS", theme.MailSendIcon(), func() {
		dev, info := ui.getSelectedModemDevice()
		if dev == nil {
			dialog.ShowInformation("Внимание", "Нет доступных подключенных модемов!", ui.win)
			return
		}

		phone := strings.TrimSpace(ui.phoneEntry.Text)
		text := strings.TrimSpace(ui.smsEntry.Text)
		if phone == "" || text == "" {
			dialog.ShowInformation("Ошибка", "Заполните номер и текст SMS", ui.win)
			return
		}

		ui.smsStatus.SetText("Отправка...")
		ui.appendLog(fmt.Sprintf("[%s] Отправка SMS на %s", info.Port, phone))

		go func() {
			err := dev.SendSMS(phone, text)
			if err != nil {
				ui.smsStatus.SetText(fmt.Sprintf("Ошибка: %v", err))
				ui.appendLog(fmt.Sprintf("[%s] Ошибка SMS: %v", info.Port, err))
			} else {
				ui.smsStatus.SetText("SMS успешно отправлено!")
				ui.smsEntry.SetText("")
				ui.appendLog(fmt.Sprintf("[%s] SMS доставлено в сеть", info.Port))
			}
		}()
	})

	header := container.NewHBox(
		widget.NewLabel("Номер телефона:"),
		container.NewGridWrap(fyne.NewSize(200, 36), ui.phoneEntry),
		btnSend,
		ui.smsStatus,
	)

	return container.NewBorder(header, nil, nil, nil, ui.smsEntry)
}

func (ui *AppUI) buildTerminalSubTab() fyne.CanvasObject {
	ui.atEntry = widget.NewEntry()
	ui.atEntry.SetPlaceHolder("например: ATI или AT+CSQ или AT^SYSCFG?")

	ui.atOutput = widget.NewMultiLineEntry()
	ui.atOutput.Wrapping = fyne.TextWrapWord

	btnSend := widget.NewButtonWithIcon("Отправить", theme.MediaPlayIcon(), func() {
		dev, info := ui.getSelectedModemDevice()
		if dev == nil {
			dialog.ShowInformation("Внимание", "Нет доступных подключенных модемов!", ui.win)
			return
		}

		cmd := strings.TrimSpace(ui.atEntry.Text)
		if cmd == "" {
			return
		}

		ui.atOutput.SetText(ui.atOutput.Text + fmt.Sprintf("\n> [%s] %s\n", info.Port, cmd))
		go func() {
			out, err := dev.ExecuteCustomAT(cmd)
			if err != nil {
				ui.atOutput.SetText(ui.atOutput.Text + fmt.Sprintf("Error: %v\n", err))
			} else {
				ui.atOutput.SetText(ui.atOutput.Text + out + "\n")
			}
		}()
	})

	top := container.NewBorder(nil, nil, widget.NewLabel("AT Команда:"), btnSend, ui.atEntry)
	return container.NewBorder(top, nil, nil, nil, ui.atOutput)
}

// -------------------------------------------------------------
// 4. Вкладка "Лог событий"
// -------------------------------------------------------------
func (ui *AppUI) buildLogTab() fyne.CanvasObject {
	ui.logOutput = widget.NewMultiLineEntry()
	ui.logOutput.Wrapping = fyne.TextWrapBreak

	btnClear := widget.NewButtonWithIcon("Очистить лог", theme.DeleteIcon(), func() {
		ui.logMutex.Lock()
		ui.logOutput.SetText("")
		ui.logMutex.Unlock()
	})

	btnExport := widget.NewButtonWithIcon("Сохранить в файл", theme.DocumentSaveIcon(), func() {
		ui.logMutex.Lock()
		text := ui.logOutput.Text
		ui.logMutex.Unlock()

		filename := fmt.Sprintf("modem_log_%s.txt", time.Now().Format("20060102_150405"))
		err := os.WriteFile(filename, []byte(text), 0644)
		if err != nil {
			dialog.ShowError(err, ui.win)
		} else {
			dialog.ShowInformation("Сохранено", fmt.Sprintf("Лог сохранен в файл:\n%s", filename), ui.win)
		}
	})

	topBar := container.NewBorder(
		nil, nil,
		widget.NewLabelWithStyle("Журнал операций модемного пула", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		container.NewHBox(btnClear, btnExport),
	)

	return container.NewBorder(topBar, nil, nil, nil, ui.logOutput)
}

// -------------------------------------------------------------
// 5. Вкладка "Настройки"
// -------------------------------------------------------------
func (ui *AppUI) buildSettingsTab() fyne.CanvasObject {
	ui.apiStatus = widget.NewLabel("🟢 Работает на 127.0.0.1:5001")
	ui.apiStatus.TextStyle = fyne.TextStyle{Bold: true}

	ui.apiCheck = widget.NewCheck("Локальный HTTP API сервер для расширения Chrome (порт 5001)", func(checked bool) {
		if ui.apiServer == nil {
			return
		}
		if checked {
			_ = ui.apiServer.Start()
			ui.apiStatus.SetText("🟢 Работает на 127.0.0.1:5001")
			ui.appendLog("API сервер запущен вручную (порт 5001)")
		} else {
			ui.apiServer.Stop()
			ui.apiStatus.SetText("⚪ Остановлен")
			ui.appendLog("API сервер остановлен пользователем")
		}
	})
	ui.apiCheck.SetChecked(true)

	ui.baudSelect = widget.NewSelect([]string{"9600", "19200", "38400", "57600", "115200", "230400"}, nil)
	ui.baudSelect.PlaceHolder = "115200"
	ui.baudSelect.SetSelected("115200")

	ui.pollInterval = widget.NewSelect([]string{"3 сек", "5 сек", "10 сек", "30 сек"}, nil)
	ui.pollInterval.PlaceHolder = "5 сек"
	ui.pollInterval.SetSelected("5 сек")

	ui.themeSelect = widget.NewRadioGroup([]string{"Темная тема", "Светлая тема"}, func(s string) {
		if s == "Темная тема" {
			ui.myApp.Settings().SetTheme(theme.DarkTheme())
		} else {
			ui.myApp.Settings().SetTheme(theme.LightTheme())
		}
		if ui.isGridView {
			ui.refreshGrid()
		}
	})
	ui.themeSelect.SetSelected("Темная тема")

	apiCard := widget.NewCard(
		"Интеграция с браузером (Chrome Extension)",
		"",
		container.NewVBox(
			ui.apiCheck,
			container.NewHBox(widget.NewLabel("Статус API:"), ui.apiStatus),
		),
	)

	form := widget.NewForm(
		widget.NewFormItem("Скорость порта (Baudrate):", ui.baudSelect),
		widget.NewFormItem("Интервал опроса состояния:", ui.pollInterval),
		widget.NewFormItem("Тема оформления:", ui.themeSelect),
	)

	aboutCard := widget.NewCard(
		"О программе",
		"Huawei E173 Multi-Modem Control Suite v1.4.0",
		widget.NewLabel("Высокопроизводительное приложение на Go + Fyne.\nПоддерживает неограниченное число модемов E173, автореконнект USB, параллельный опрос и PDU/UCS2."),
	)

	return container.NewVBox(
		apiCard,
		widget.NewSeparator(),
		widget.NewLabelWithStyle("Конфигурация модемов", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		form,
		widget.NewSeparator(),
		aboutCard,
	)
}
