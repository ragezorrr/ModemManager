// Пересылаем запросы через background сервис-воркер, где нет ограничений PNA
chrome.runtime.onMessage.addListener((request, sender, sendResponse) => {
  if (request.action === "getPortData") {
    fetch('http://127.0.0.1:5001/api/port1?_=' + Date.now())
      .then(res => res.json())
      .then(data => sendResponse({ success: true, data: data }))
      .catch(err => sendResponse({ success: false, error: err.toString() }));
    return true; // Асинхронный ответ
  }
});
