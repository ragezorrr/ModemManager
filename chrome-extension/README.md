# Binotel Port Fixer (E173 Edition)

Точная реализация расширения для страницы редактирования GSM-портов Binotel:
`https://panel.binotel.com/?module=gsmPorts&action=edit&companyID=*`

## Архитектура:
* **Background Service Worker (`background.js`):** пересылает запросы на локальный бэкенд `http://127.0.0.1:5001/api/port1` в обход браузерных ограничений Private Network Access (PNA) и Mixed Content.
* **Content Script (`content.js`):** отрисовывает компактный виджет в правом нижнем углу (стиль Catppuccin Mocha), опрашивает данные каждые 150 мс и копирует IMEI в буфер обмена по клику.
* **Backend (`e173-control`):** встроенный высокоскоростной API на порту `5001`.

## Установка в Chrome:
1. Откройте `chrome://extensions/`.
2. Включите **«Режим разработчика»** в правом верхнем углу.
3. Нажмите **«Загрузить распакованное расширение»** и выберите папку:
   ```
   /home/anc/e173-control/chrome-extension
   ```
   *(Если расширение уже добавлено, нажмите значок круговой стрелки «Обновить» 🔄).*
