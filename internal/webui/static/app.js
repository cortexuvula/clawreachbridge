(function() {
    'use strict';

    var API = '/api/v1';
    var dashboardTimer = null;
    var connectionsTimer = null;
    var logsTimer = null;
    var lastLogTime = '';

    // Tab navigation
    document.querySelectorAll('.tab').forEach(function(btn) {
        btn.addEventListener('click', function() {
            var tab = btn.getAttribute('data-tab');
            document.querySelectorAll('.tab').forEach(function(t) { t.classList.remove('active'); });
            document.querySelectorAll('.tab-content').forEach(function(t) { t.classList.remove('active'); });
            btn.classList.add('active');
            document.getElementById('tab-' + tab).classList.add('active');
            stopAllTimers();
            startTimers(tab);
        });
    });

    function stopAllTimers() {
        if (dashboardTimer) { clearInterval(dashboardTimer); dashboardTimer = null; }
        if (connectionsTimer) { clearInterval(connectionsTimer); connectionsTimer = null; }
        if (logsTimer) { clearInterval(logsTimer); logsTimer = null; }
    }

    function startTimers(tab) {
        if (tab === 'dashboard') {
            fetchStatus();
            dashboardTimer = setInterval(fetchStatus, 3000);
        } else if (tab === 'connections') {
            fetchConnections();
            connectionsTimer = setInterval(fetchConnections, 5000);
        } else if (tab === 'config') {
            fetchConfig();
        } else if (tab === 'logs') {
            lastLogTime = '';
            fetchLogs(false);
            logsTimer = setInterval(function() {
                if (document.getElementById('log-auto-refresh').checked) {
                    fetchLogs(true);
                }
            }, 3000);
        }
    }

    // Dashboard
    function fetchStatus() {
        fetch(API + '/status').then(function(r) { return r.json(); }).then(function(d) {
            setText('uptime', d.uptime);
            setText('active-conns', String(d.active_connections));
            setText('total-conns', String(d.total_connections));
            setText('total-msgs', String(d.total_messages));
            setText('memory', d.memory_mb.toFixed(1) + ' MB');
            setText('goroutines', String(d.goroutines));
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
            bi.textContent = d.git_commit.substring(0, 8) + '\n' + d.build_time;
        }).catch(function() {});
    }

    // Connections
    function fetchConnections() {
        fetch(API + '/connections').then(function(r) { return r.json(); }).then(function(data) {
            var tbody = document.getElementById('connections-body');
            // Clear existing rows safely
            while (tbody.firstChild) { tbody.removeChild(tbody.firstChild); }

            if (!data || data.length === 0) {
                var tr = document.createElement('tr');
                var td = document.createElement('td');
                td.setAttribute('colspan', '2');
                td.className = 'empty';
                td.textContent = 'No active connections';
                tr.appendChild(td);
                tbody.appendChild(tr);
                return;
            }
            data.forEach(function(c) {
                var tr = document.createElement('tr');
                var tdIP = document.createElement('td');
                tdIP.textContent = c.ip;
                var tdCount = document.createElement('td');
                tdCount.textContent = String(c.count);
                tr.appendChild(tdIP);
                tr.appendChild(tdCount);
                tbody.appendChild(tr);
            });
        }).catch(function() {});
    }

    // Config
    function fetchConfig() {
        fetch(API + '/config').then(function(r) { return r.json(); }).then(function(d) {
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
            // Clear and rebuild with safe DOM methods
            while (roEl.firstChild) { roEl.removeChild(roEl.firstChild); }
            appendRORow(roEl, 'Listen Address', ro.listen_address);
            appendRORow(roEl, 'Gateway URL', ro.gateway_url);
            appendRORow(roEl, 'Origin', ro.origin);
            appendRORow(roEl, 'Health Address', ro.health_address);
            appendRORow(roEl, 'Tailscale Only', String(ro.tailscale_only));
            appendRORow(roEl, 'TLS Enabled', String(ro.tls_enabled));
            appendRORow(roEl, 'Auth Token', rl.auth_token_set ? 'set' : 'not set');
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
            var el = document.getElementById('config-status');
            if (res.ok) {
                el.textContent = 'Applied';
                el.className = 'status-msg ok';
            } else {
                el.textContent = res.data.error || 'Failed';
                el.className = 'status-msg err';
            }
            setTimeout(function() { el.textContent = ''; }, 3000);
        }).catch(function() {
            var el = document.getElementById('config-status');
            el.textContent = 'Network error';
            el.className = 'status-msg err';
        });
    });

    // Logs
    function fetchLogs(incremental) {
        var level = document.getElementById('log-level-filter').value;
        var limit = document.getElementById('log-limit').value || '100';
        var url = API + '/logs?level=' + level + '&limit=' + limit;
        if (incremental && lastLogTime) {
            url += '&since=' + encodeURIComponent(lastLogTime);
        }

        fetch(url).then(function(r) { return r.json(); }).then(function(entries) {
            var viewer = document.getElementById('log-viewer');

            if (!entries || entries.length === 0) {
                if (!incremental) {
                    while (viewer.firstChild) { viewer.removeChild(viewer.firstChild); }
                    var emptyDiv = document.createElement('div');
                    emptyDiv.className = 'empty';
                    emptyDiv.textContent = 'No log entries';
                    viewer.appendChild(emptyDiv);
                }
                return;
            }

            // Track newest timestamp for incremental fetches
            if (entries.length > 0) {
                lastLogTime = entries[0].time;
            }

            // Build log lines as DOM elements
            var fragment = document.createDocumentFragment();
            entries.forEach(function(e) {
                var div = document.createElement('div');
                div.className = 'log-line';

                var timeSpan = document.createElement('span');
                timeSpan.className = 'log-time';
                timeSpan.textContent = e.time.substring(11, 23) + ' ';
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
                while (viewer.firstChild) { viewer.removeChild(viewer.firstChild); }
                viewer.appendChild(fragment);
            }
        }).catch(function() {});
    }

    document.getElementById('log-level-filter').addEventListener('change', function() {
        lastLogTime = '';
        fetchLogs(false);
    });

    // Controls
    document.getElementById('btn-reload').addEventListener('click', function() {
        var btn = this;
        btn.disabled = true;
        fetch(API + '/reload', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' }
        }).then(function(r) { return r.json().then(function(d) { return { ok: r.ok, data: d }; }); })
        .then(function(res) {
            var el = document.getElementById('reload-status');
            if (res.ok) {
                el.textContent = 'Reloaded successfully';
                el.className = 'status-msg ok';
            } else {
                el.textContent = res.data.error || 'Reload failed';
                el.className = 'status-msg err';
            }
            btn.disabled = false;
            setTimeout(function() { el.textContent = ''; }, 5000);
        }).catch(function() {
            btn.disabled = false;
            var el = document.getElementById('reload-status');
            el.textContent = 'Network error';
            el.className = 'status-msg err';
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
            var el = document.getElementById('restart-status');
            el.textContent = 'Restarting... page will reconnect.';
            el.className = 'status-msg ok';
        }).catch(function() {
            var el = document.getElementById('restart-status');
            el.textContent = 'Restart signal sent';
            el.className = 'status-msg ok';
        });
    });

    // Helpers
    function setText(id, val) {
        document.getElementById(id).textContent = val;
    }

    function pad(s, len) {
        while (s.length < len) s += ' ';
        return s;
    }

    // Start dashboard on load
    startTimers('dashboard');
})();
