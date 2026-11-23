(function() {
    'use strict';

    // 1. Создаем виджет
    const container = document.createElement('div');
    Object.assign(container.style, {
        position: 'fixed',
        bottom: '20px',
        right: '20px',
        zIndex: '999999',
        backgroundColor: '#1e1e2e',
        color: '#cdd6f4',
        padding: '12px 16px',
        borderRadius: '8px',
        boxShadow: '0 4px 12px rgba(0, 0, 0, 0.4)',
        fontFamily: 'monospace, sans-serif',
        fontSize: '13px',
        lineHeight: '1.5',
        border: '1px solid #45475a',
        minWidth: '230px',
        userSelect: 'none'
    });

    document.body.appendChild(container);

    let lastSnapshot = "";
    let isCopiedTooltipActive = false;

    // 2. Делегирование события клика (копирование в буфер)
    container.addEventListener('click', (e) => {
        const imeiBtn = e.target.closest('#tm-imei-btn');
        if (!imeiBtn) return;

        const imeiText = imeiBtn.getAttribute('data-imei');
        if (!imeiText) return;

        // Копирование в буфер обмена
        navigator.clipboard.writeText(imeiText).then(() => {
            isCopiedTooltipActive = true;

            imeiBtn.style.color = '#a6e3a1';
            imeiBtn.innerText = 'Скопировано! ✓';

            setTimeout(() => {
                isCopiedTooltipActive = false;
                lastSnapshot = "";
            }, 1000);
        }).catch(err => {
            console.error('Ошибка копирования:', err);
        });
    });

    function renderPortBlock(data, isLast) {
        const imei = data.imei && data.imei !== '—' ? data.imei : null;
        const isConnected = data.status === 'Connected';
        const statusColor = isConnected ? '#a6e3a1' : '#f38ba8';
        const borderBottom = isLast ? '' : 'border-bottom: 1px dashed #45475a; padding-bottom: 8px; margin-bottom: 8px;';

        return `
        <div style="${borderBottom}">
          <div style="font-weight:bold; margin-bottom:6px; border-bottom:1px solid #45475a; padding-bottom:4px; display:flex; justify-content:space-between; align-items:center;">
            <span>Порт: ${data.port || '—'} ${data.phone_number && data.phone_number !== '—' ? `<span style="color:#a6adc8; font-size:11px; margin-left:4px;">(${data.phone_number})</span>` : ''}</span>
            <span style="color:${statusColor}; font-weight:bold;">● ${data.status || 'Unknown'}</span>
          </div>
          <div style="display:flex; justify-content:space-between; align-items:center; margin-bottom:2px;">
            <span>IMEI:</span>
            ${imei ? `
              <span id="tm-imei-btn" data-imei="${imei}" title="Нажмите, чтобы скопировать" style="
                color: #89b4fa;
                font-weight: bold;
                cursor: pointer;
                background: #313244;
                padding: 2px 6px;
                border-radius: 4px;
                border: 1px solid #45475a;
                transition: background 0.2s;
              " onmouseover="this.style.background='#45475a'" onmouseout="this.style.background='#313244'">
                ${imei} 📋
              </span>
            ` : `
              <span style="color: #f38ba8; font-weight: bold;">Поиск...</span>
            `}
          </div>
          <div style="display:flex; justify-content:space-between;">
            <span>Оператор:</span>
            <span style="color: ${data.operator === 'No Service' || data.operator === 'No SIM' ? '#fab387' : '#cdd6f4'}">${data.operator || '—'}</span>
          </div>
        </div>
        `;
    }

    // 3. Опрос background.js
    function poll() {
        chrome.runtime.sendMessage({ action: "getPortData" }, (response) => {
            if (response && response.success) {
                const data = response.data;
                const snapshot = JSON.stringify(data);

                if (!isCopiedTooltipActive && lastSnapshot !== snapshot) {
                    lastSnapshot = snapshot;

                    if (Array.isArray(data)) {
                        let html = '';
                        data.forEach((item, idx) => {
                            html += renderPortBlock(item, idx === data.length - 1);
                        });
                        container.innerHTML = html;
                    } else if (data) {
                        container.innerHTML = renderPortBlock(data, true);
                    }
                }
            }
            setTimeout(poll, 150);
        });
    }

    poll();
})();
