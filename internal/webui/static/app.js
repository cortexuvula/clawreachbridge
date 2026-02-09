(function() {
    'use strict';

    var API = '/api/v1';
    var SVG_NS = 'http://www.w3.org/2000/svg';

    // ─── State ───────────────────────────────────────────────────────
    var State = {
        activeTab: 'dashboard',
        timers: { dashboard: null, connections: null, logs: null, connMonitor: null },
        lastLogTime: '',
        apiReachable: true,
        reconnectAttempts: 0,
        theme: localStorage.getItem('crb-theme') || 'dark',
        firstLoad: { dashboard: true, connections: true, logs: true, config: true }
    };

    // ─── Toast ───────────────────────────────────────────────────────
    var Toast = {
        icons: { success: '\u2713', error: '\u2717', warning: '\u26A0', info: '\u2139' },

        show: function(message, type, duration) {
            type = type || 'info';
            duration = duration || 4000;
            var container = document.getElementById('toast-container');
            var el = document.createElement('div');
            el.className = 'toast toast-' + type;

            var icon = document.createElement('span');
            icon.className = 'toast-icon';
            icon.textContent = Toast.icons[type] || '';
            el.appendChild(icon);

            var text = document.createElement('span');
            text.textContent = message;
            el.appendChild(text);

            el.addEventListener('click', function() { Toast.dismiss(el); });
            container.appendChild(el);

            setTimeout(function() { Toast.dismiss(el); }, duration);
            return el;
        },

        dismiss: function(el) {
            if (!el || !el.parentNode) return;
            el.classList.add('toast-exit');
            setTimeout(function() {
                if (el.parentNode) el.parentNode.removeChild(el);
            }, 200);
        }
    };

    // ─── Theme ───────────────────────────────────────────────────────
    var Theme = {
        init: function() {
            document.documentElement.setAttribute('data-theme', State.theme);
            Theme.updateIcon();
            document.getElementById('theme-toggle').addEventListener('click', Theme.toggle);
        },

        toggle: function() {
            State.theme = State.theme === 'dark' ? 'light' : 'dark';
            document.documentElement.setAttribute('data-theme', State.theme);
            localStorage.setItem('crb-theme', State.theme);
            Theme.updateIcon();
        },

        updateIcon: function() {
            var btn = document.getElementById('theme-toggle');
            // Moon for dark, sun for light
            btn.textContent = State.theme === 'dark' ? '\u263E' : '\u2600';
            btn.title = State.theme === 'dark' ? 'Switch to light theme' : 'Switch to dark theme';
        }
    };

    // ─── ConnMonitor ─────────────────────────────────────────────────
    var ConnMonitor = {
        start: function() {
            ConnMonitor.check();
            State.timers.connMonitor = setInterval(ConnMonitor.check, 10000);
        },

        check: function() {
            fetch(API + '/status').then(function(r) {
                if (!r.ok) throw new Error('not ok');
                return r.json();
            }).then(function() {
                if (!State.apiReachable) {
                    State.apiReachable = true;
                    State.reconnectAttempts = 0;
                    ConnMonitor.updateUI();
                    Toast.show('API connection restored', 'success');
                }
            }).catch(function() {
                if (State.apiReachable) {
                    State.apiReachable = false;
                }
                State.reconnectAttempts++;
                ConnMonitor.updateUI();
            });
        },

        updateUI: function() {
            var dot = document.getElementById('conn-indicator');
            var banner = document.getElementById('reconnect-banner');
            var count = document.getElementById('reconnect-count');

            if (State.apiReachable) {
                dot.className = 'conn-dot conn-online';
                dot.title = 'API connected';
                banner.style.display = 'none';
            } else {
                dot.className = 'conn-dot conn-offline';
                dot.title = 'API unreachable';
                banner.style.display = '';
                count.textContent = String(State.reconnectAttempts);
            }
        }
    };

    // ─── HistoryBuffer ───────────────────────────────────────────────
    var HistoryBuffer = {
        maxLen: 60,
        data: {
            activeConns: [],
            totalConns: [],
            totalMsgs: [],
            memory: [],
            goroutines: []
        },

        push: function(key, value) {
            var arr = HistoryBuffer.data[key];
            if (!arr) return;
            arr.push(value);
            if (arr.length > HistoryBuffer.maxLen) arr.shift();
        }
    };

    // ─── Sparkline ───────────────────────────────────────────────────
    var Sparkline = {
        render: function(containerId, values, color) {
            var container = document.getElementById(containerId);
            if (!container || values.length < 2) return;

            while (container.firstChild) container.removeChild(container.firstChild);

            var w = container.clientWidth || 160;
            var h = 32;
            var svg = document.createElementNS(SVG_NS, 'svg');
            svg.setAttribute('viewBox', '0 0 ' + w + ' ' + h);
            svg.setAttribute('preserveAspectRatio', 'none');

            var min = Math.min.apply(null, values);
            var max = Math.max.apply(null, values);
            var range = max - min || 1;
            var pad = 2;

            var points = [];
            for (var i = 0; i < values.length; i++) {
                var x = (i / (values.length - 1)) * w;
                var y = h - pad - ((values[i] - min) / range) * (h - 2 * pad);
                points.push(x.toFixed(1) + ',' + y.toFixed(1));
            }

            // Gradient fill
            var defs = document.createElementNS(SVG_NS, 'defs');
            var gradId = containerId + '-grad';
            var grad = document.createElementNS(SVG_NS, 'linearGradient');
            grad.setAttribute('id', gradId);
            grad.setAttribute('x1', '0');
            grad.setAttribute('y1', '0');
            grad.setAttribute('x2', '0');
            grad.setAttribute('y2', '1');

            var stop1 = document.createElementNS(SVG_NS, 'stop');
            stop1.setAttribute('offset', '0%');
            stop1.setAttribute('stop-color', color);
            stop1.setAttribute('stop-opacity', '0.3');
            var stop2 = document.createElementNS(SVG_NS, 'stop');
            stop2.setAttribute('offset', '100%');
            stop2.setAttribute('stop-color', color);
            stop2.setAttribute('stop-opacity', '0.02');
            grad.appendChild(stop1);
            grad.appendChild(stop2);
            defs.appendChild(grad);
            svg.appendChild(defs);

            // Fill area
            var areaPoints = '0,' + h + ' ' + points.join(' ') + ' ' + w + ',' + h;
            var area = document.createElementNS(SVG_NS, 'polygon');
            area.setAttribute('points', areaPoints);
            area.setAttribute('fill', 'url(#' + gradId + ')');
            svg.appendChild(area);

            // Line
            var polyline = document.createElementNS(SVG_NS, 'polyline');
            polyline.setAttribute('points', points.join(' '));
            polyline.setAttribute('fill', 'none');
            polyline.setAttribute('stroke', color);
            polyline.setAttribute('stroke-width', '1.5');
            polyline.setAttribute('stroke-linejoin', 'round');
            svg.appendChild(polyline);

            // Current value dot
            var lastParts = points[points.length - 1].split(',');
            var dot = document.createElementNS(SVG_NS, 'circle');
            dot.setAttribute('cx', lastParts[0]);
            dot.setAttribute('cy', lastParts[1]);
            dot.setAttribute('r', '2.5');
            dot.setAttribute('fill', color);
            svg.appendChild(dot);

            container.appendChild(svg);
        }
    };

    // ─── Clipboard ───────────────────────────────────────────────────
    var Clipboard = {
        copy: function(text, label) {
            if (!navigator.clipboard) {
                Toast.show('Clipboard not available', 'warning');
                return;
            }
            navigator.clipboard.writeText(text).then(function() {
                Toast.show('Copied ' + (label || 'value'), 'success', 2000);
            }).catch(function() {
                Toast.show('Copy failed', 'error', 2000);
            });
        },

        createBtn: function(getText, label) {
            var btn = document.createElement('button');
            btn.className = 'copy-btn';
            btn.textContent = 'copy';
            btn.type = 'button';
            btn.addEventListener('click', function(e) {
                e.stopPropagation();
                Clipboard.copy(getText(), label);
            });
            return btn;
        }
    };

    // ─── Keyboard ────────────────────────────────────────────────────
    var Keyboard = {
        init: function() {
            document.addEventListener('keydown', function(e) {
                // Ctrl+1-5 switches tabs
                if (e.ctrlKey && !e.shiftKey && !e.altKey && !e.metaKey) {
                    var tabs = ['dashboard', 'connections', 'config', 'logs', 'controls'];
                    var idx = parseInt(e.key, 10) - 1;
                    if (idx >= 0 && idx < tabs.length) {
                        e.preventDefault();
                        switchTab(tabs[idx]);
                    }
                }
                // Escape dismisses topmost toast
                if (e.key === 'Escape') {
                    var container = document.getElementById('toast-container');
                    var first = container.firstElementChild;
                    if (first) Toast.dismiss(first);
                }
            });
        }
    };

    // ─── Helpers ─────────────────────────────────────────────────────
    function formatNumber(n) {
        if (typeof n !== 'number') return String(n);
        return n.toLocaleString();
    }

    function setText(id, val) {
        var el = document.getElementById(id);
        if (!el) return;
        var str = String(val);
        if (el.textContent !== str) {
            el.textContent = str;
            el.classList.remove('value-flash');
            // Force reflow so animation restarts
            void el.offsetWidth;
            el.classList.add('value-flash');
        }
    }

    function pad(s, len) {
        while (s.length < len) s += ' ';
        return s;
    }

    function relativeTime(iso) {
        if (!iso) return '';
        var then = new Date(iso).getTime();
        var now = Date.now();
        var sec = Math.floor((now - then) / 1000);
        if (sec < 5) return 'just now';
        if (sec < 60) return sec + 's ago';
        var min = Math.floor(sec / 60);
        if (min < 60) return min + 'm ago';
        var hr = Math.floor(min / 60);
        if (hr < 24) return hr + 'h ago';
        return Math.floor(hr / 24) + 'd ago';
    }

    function showLoading(containerId) {
        var el = document.getElementById(containerId);
        if (!el) return;
        while (el.firstChild) el.removeChild(el.firstChild);
        var overlay = document.createElement('div');
        overlay.className = 'loading-overlay';
        var spinner = document.createElement('div');
        spinner.className = 'spinner';
        overlay.appendChild(spinner);
        el.appendChild(overlay);
    }

    // ─── Tab Navigation ──────────────────────────────────────────────
    function switchTab(name) {
        State.activeTab = name;
        document.querySelectorAll('.tab').forEach(function(t) {
            var tabName = t.getAttribute('data-tab');
            if (tabName === name) {
                t.classList.add('active');
            } else {
                t.classList.remove('active');
            }
        });
        document.querySelectorAll('.tab-content').forEach(function(t) {
            t.classList.remove('active');
        });
        var section = document.getElementById('tab-' + name);
        if (section) section.classList.add('active');

        stopTabTimers();
        startTimers(name);
    }

    document.querySelectorAll('.tab').forEach(function(btn) {
        btn.addEventListener('click', function() {
            switchTab(btn.getAttribute('data-tab'));
        });
    });

    function stopTabTimers() {
        if (State.timers.dashboard) { clearInterval(State.timers.dashboard); State.timers.dashboard = null; }
        if (State.timers.connections) { clearInterval(State.timers.connections); State.timers.connections = null; }
        if (State.timers.logs) { clearInterval(State.timers.logs); State.timers.logs = null; }
    }

    function startTimers(tab) {
        if (tab === 'dashboard') {
            fetchStatus();
            State.timers.dashboard = setInterval(fetchStatus, 3000);
        } else if (tab === 'connections') {
            if (State.firstLoad.connections) {
                showLoading('connections-body');
            }
            fetchConnections();
            State.timers.connections = setInterval(fetchConnections, 5000);
        } else if (tab === 'config') {
            fetchConfig();
        } else if (tab === 'logs') {
            State.lastLogTime = '';
            if (State.firstLoad.logs) {
                showLoading('log-viewer');
            }
            fetchLogs(false);
            State.timers.logs = setInterval(function() {
                if (document.getElementById('log-auto-refresh').checked) {
                    fetchLogs(true);
                }
            }, 3000);
        }
    }

    // ─── Dashboard ───────────────────────────────────────────────────
    function fetchStatus() {
        fetch(API + '/status').then(function(r) { return r.json(); }).then(function(d) {
            State.apiReachable = true;
            State.firstLoad.dashboard = false;

            setText('uptime', d.uptime);
            setText('active-conns', formatNumber(d.active_connections));
            setText('total-conns', formatNumber(d.total_connections));
            setText('total-msgs', formatNumber(d.total_messages));
            setText('memory', d.memory_mb.toFixed(1) + ' MB');
            setText('goroutines', formatNumber(d.goroutines));
            setText('version', 'v' + d.version);

            var gs = document.getElementById('gateway-status');
            if (d.gateway_reachable) {
                gs.textContent = 'Connected';
                gs.className = 'card-value status-ok';
            } else {
                gs.textContent = 'Unreachable';
                gs.className = 'card-value status-degraded';
            }

            var bi = document.getElementById('build-info');
            var commitShort = d.git_commit.substring(0, 8);
            bi.textContent = commitShort + '\n' + d.build_time;

            // Update connection tab badge
            var badge = document.getElementById('conn-tab-badge');
            if (d.active_connections > 0) {
                badge.textContent = String(d.active_connections);
                badge.style.display = '';
            } else {
                badge.style.display = 'none';
            }

            // Push to history and render sparklines
            HistoryBuffer.push('activeConns', d.active_connections);
            HistoryBuffer.push('totalConns', d.total_connections);
            HistoryBuffer.push('totalMsgs', d.total_messages);
            HistoryBuffer.push('memory', d.memory_mb);
            HistoryBuffer.push('goroutines', d.goroutines);

            renderSparklines();
        }).catch(function() {});
    }

    function renderSparklines() {
        var map = {
            'spark-active-conns': { data: 'activeConns', color: 'var(--primary)' },
            'spark-total-conns': { data: 'totalConns', color: 'var(--green)' },
            'spark-total-msgs': { data: 'totalMsgs', color: 'var(--yellow)' },
            'spark-memory': { data: 'memory', color: 'var(--orange)' },
            'spark-goroutines': { data: 'goroutines', color: 'var(--purple)' }
        };

        for (var id in map) {
            var info = map[id];
            var values = HistoryBuffer.data[info.data];
            if (values.length >= 2) {
                // Resolve CSS var to computed color for SVG
                var color = info.color;
                var container = document.getElementById(id);
                if (container) {
                    var computed = getComputedStyle(container.closest('.card')).getPropertyValue(
                        info.color.replace('var(', '').replace(')', '')
                    ).trim();
                    if (computed) color = computed;
                }
                Sparkline.render(id, values, color);
            }
        }
    }

    // ─── Connections ─────────────────────────────────────────────────
    function fetchConnections() {
        fetch(API + '/connections').then(function(r) { return r.json(); }).then(function(data) {
            State.firstLoad.connections = false;
            var tbody = document.getElementById('connections-body');
            while (tbody.firstChild) tbody.removeChild(tbody.firstChild);

            if (!data || data.length === 0) {
                var tr = document.createElement('tr');
                var td = document.createElement('td');
                td.setAttribute('colspan', '2');
                td.className = 'empty';

                var emptyDiv = document.createElement('div');
                emptyDiv.className = 'empty-state';
                var iconDiv = document.createElement('div');
                iconDiv.className = 'empty-state-icon';
                iconDiv.textContent = '\u2014';
                var titleDiv = document.createElement('div');
                titleDiv.className = 'empty-state-title';
                titleDiv.textContent = 'No active connections';
                var subDiv = document.createElement('div');
                subDiv.className = 'empty-state-sub';
                subDiv.textContent = 'Connections will appear here when clients connect';
                emptyDiv.appendChild(iconDiv);
                emptyDiv.appendChild(titleDiv);
                emptyDiv.appendChild(subDiv);
                td.appendChild(emptyDiv);
                tr.appendChild(td);
                tbody.appendChild(tr);
                return;
            }

            data.forEach(function(c) {
                var tr = document.createElement('tr');
                var tdIP = document.createElement('td');
                tdIP.textContent = c.ip;
                tdIP.appendChild(Clipboard.createBtn(function() { return c.ip; }, 'IP'));
                var tdCount = document.createElement('td');
                tdCount.textContent = String(c.count);
                tr.appendChild(tdIP);
                tr.appendChild(tdCount);
                tbody.appendChild(tr);
            });
        }).catch(function() {});
    }

    // ─── Config ──────────────────────────────────────────────────────
    function fetchConfig() {
        fetch(API + '/config').then(function(r) { return r.json(); }).then(function(d) {
            State.firstLoad.config = false;
            var rl = d.reloadable;
            document.getElementById('cfg-log-level').value = rl.log_level;
            document.getElementById('cfg-max-conns').value = rl.max_connections;
            document.getElementById('cfg-max-conns-ip').value = rl.max_connections_per_ip;
            document.getElementById('cfg-max-msg').value = rl.max_message_size;
            document.getElementById('cfg-rate-enabled').checked = rl.rate_limit_enabled;
            document.getElementById('cfg-conns-min').value = rl.connections_per_minute;
            document.getElementById('cfg-msgs-sec').value = rl.messages_per_second;

            var ro = d.read_only;
            var roEl = document.getElementById('config-readonly');
            while (roEl.firstChild) roEl.removeChild(roEl.firstChild);

            var roFields = [
                ['Listen Address', ro.listen_address],
                ['Gateway URL', ro.gateway_url],
                ['Origin', ro.origin],
                ['Health Address', ro.health_address],
                ['Tailscale Only', String(ro.tailscale_only)],
                ['TLS Enabled', String(ro.tls_enabled)],
                ['Auth Token', rl.auth_token_set ? 'set' : 'not set']
            ];

            roFields.forEach(function(field) {
                appendRORow(roEl, field[0], field[1]);
            });
        }).catch(function() {});
    }

    function appendRORow(parent, label, value) {
        var row = document.createElement('div');
        row.className = 'ro-row';
        var keySpan = document.createElement('span');
        keySpan.className = 'ro-key';
        keySpan.textContent = label;
        var valSpan = document.createElement('span');
        valSpan.textContent = value;
        valSpan.appendChild(Clipboard.createBtn(function() { return value; }, label));
        row.appendChild(keySpan);
        row.appendChild(valSpan);
        parent.appendChild(row);
    }

    document.getElementById('config-form').addEventListener('submit', function(e) {
        e.preventDefault();
        var body = {
            log_level: document.getElementById('cfg-log-level').value,
            max_connections: parseInt(document.getElementById('cfg-max-conns').value, 10),
            max_connections_per_ip: parseInt(document.getElementById('cfg-max-conns-ip').value, 10),
            max_message_size: parseInt(document.getElementById('cfg-max-msg').value, 10),
            rate_limit_enabled: document.getElementById('cfg-rate-enabled').checked,
            connections_per_minute: parseInt(document.getElementById('cfg-conns-min').value, 10),
            messages_per_second: parseInt(document.getElementById('cfg-msgs-sec').value, 10)
        };
        fetch(API + '/config', {
            method: 'PUT',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(body)
        }).then(function(r) { return r.json().then(function(d) { return { ok: r.ok, data: d }; }); })
        .then(function(res) {
            if (res.ok) {
                Toast.show('Configuration applied', 'success');
            } else {
                Toast.show(res.data.error || 'Apply failed', 'error');
            }
        }).catch(function() {
            Toast.show('Network error', 'error');
        });
    });

    // ─── Logs ────────────────────────────────────────────────────────
    function filterLogsByText(entries) {
        var search = document.getElementById('log-search').value.trim().toLowerCase();
        if (!search) return entries;
        return entries.filter(function(e) {
            var text = (e.message || '').toLowerCase();
            if (text.indexOf(search) !== -1) return true;
            if (e.attrs) {
                for (var k in e.attrs) {
                    if (String(e.attrs[k]).toLowerCase().indexOf(search) !== -1) return true;
                }
            }
            return false;
        });
    }

    function fetchLogs(incremental) {
        var level = document.getElementById('log-level-filter').value;
        var limit = document.getElementById('log-limit').value || '100';
        var url = API + '/logs?level=' + level + '&limit=' + limit;
        if (incremental && State.lastLogTime) {
            url += '&since=' + encodeURIComponent(State.lastLogTime);
        }

        fetch(url).then(function(r) { return r.json(); }).then(function(entries) {
            State.firstLoad.logs = false;
            var viewer = document.getElementById('log-viewer');

            if (!entries || entries.length === 0) {
                if (!incremental) {
                    while (viewer.firstChild) viewer.removeChild(viewer.firstChild);
                    var emptyDiv = document.createElement('div');
                    emptyDiv.className = 'empty-state';
                    var iconDiv = document.createElement('div');
                    iconDiv.className = 'empty-state-icon';
                    iconDiv.textContent = '\u2014';
                    var titleDiv = document.createElement('div');
                    titleDiv.className = 'empty-state-title';
                    titleDiv.textContent = 'No log entries';
                    emptyDiv.appendChild(iconDiv);
                    emptyDiv.appendChild(titleDiv);
                    viewer.appendChild(emptyDiv);
                }
                return;
            }

            if (entries.length > 0) {
                State.lastLogTime = entries[0].time;
            }

            entries = filterLogsByText(entries);

            var fragment = document.createDocumentFragment();
            entries.forEach(function(e) {
                var div = document.createElement('div');
                div.className = 'log-line';

                var timeSpan = document.createElement('span');
                timeSpan.className = 'log-time';
                timeSpan.textContent = relativeTime(e.time) + ' ';
                timeSpan.title = e.time;
                div.appendChild(timeSpan);

                var levelSpan = document.createElement('span');
                levelSpan.className = 'log-level-' + e.level;
                levelSpan.textContent = pad(e.level, 5) + ' ';
                div.appendChild(levelSpan);

                var msgText = document.createTextNode(e.message);
                div.appendChild(msgText);

                if (e.attrs) {
                    var pairs = [];
                    for (var k in e.attrs) {
                        pairs.push(k + '=' + JSON.stringify(e.attrs[k]));
                    }
                    if (pairs.length > 0) {
                        var attrSpan = document.createElement('span');
                        attrSpan.className = 'log-time';
                        attrSpan.textContent = ' ' + pairs.join(' ');
                        div.appendChild(attrSpan);
                    }
                }

                fragment.appendChild(div);
            });

            if (incremental) {
                viewer.insertBefore(fragment, viewer.firstChild);
            } else {
                while (viewer.firstChild) viewer.removeChild(viewer.firstChild);
                viewer.appendChild(fragment);
            }
        }).catch(function() {});
    }

    document.getElementById('log-level-filter').addEventListener('change', function() {
        State.lastLogTime = '';
        fetchLogs(false);
    });

    document.getElementById('log-search').addEventListener('input', function() {
        // Re-fetch with filter applied
        State.lastLogTime = '';
        fetchLogs(false);
    });

    // ─── Controls ────────────────────────────────────────────────────
    document.getElementById('btn-reload').addEventListener('click', function() {
        var btn = this;
        btn.disabled = true;
        fetch(API + '/reload', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' }
        }).then(function(r) { return r.json().then(function(d) { return { ok: r.ok, data: d }; }); })
        .then(function(res) {
            if (res.ok) {
                Toast.show('Configuration reloaded', 'success');
            } else {
                Toast.show(res.data.error || 'Reload failed', 'error');
            }
            btn.disabled = false;
        }).catch(function() {
            btn.disabled = false;
            Toast.show('Network error', 'error');
        });
    });

    document.getElementById('btn-restart').addEventListener('click', function() {
        if (!confirm('Are you sure you want to restart the service?')) return;
        var btn = this;
        btn.disabled = true;
        fetch(API + '/restart', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' }
        }).then(function() {
            Toast.show('Restarting... page will reconnect.', 'info', 8000);
        }).catch(function() {
            Toast.show('Restart signal sent', 'info', 8000);
        });
    });

    // ─── Init ────────────────────────────────────────────────────────
    Theme.init();
    Keyboard.init();
    ConnMonitor.start();
    startTimers('dashboard');
})();
