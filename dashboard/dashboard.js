// Packet Wire Dashboard - D3 Network Visualization with WebSocket

class NetworkDashboard {
    constructor() {
        this.nodes = [];
        this.edges = [];
        this.nodeMap = new Map();
        this.svg = null;
        this.simulation = null;
        this.ws = null;
        this.wsReconnectDelay = 1000;
        this.selectedNode = null;
        this.viewMode = 'grouped'; // 'individual' or 'grouped'
        
        this.width = 0;
        this.height = 0;
        
        this.init();
    }
    
    init() {
        this.setupThemeToggle();
        this.setupViewModeToggle();
        this.setupSVG();
        this.connectWebSocket();
        
        // Handle window resize
        window.addEventListener('resize', () => this.handleResize());
    }
    
    setupViewModeToggle() {
        const toggle = document.getElementById('viewModeToggle');
        if (!toggle) return;
        
        toggle.addEventListener('click', () => {
            this.viewMode = this.viewMode === 'grouped' ? 'individual' : 'grouped';
            toggle.textContent = this.viewMode === 'grouped' ? '📊 Grouped' : '📋 Individual';
            toggle.title = this.viewMode === 'grouped' ? 'Switch to individual view' : 'Switch to grouped view';
            this.fetchNetworkData();
        });
    }
    
    async fetchNetworkData() {
        try {
            const endpoint = this.viewMode === 'grouped' ? '/api/network-grouped' : '/api/network';
            const response = await fetch(endpoint);
            const data = await response.json();
            
            if (this.viewMode === 'grouped') {
                this.handleGroupedTopology(data);
            } else {
                this.handleFullTopology(data);
            }
        } catch (error) {
            console.error('Failed to fetch network data:', error);
        }
    }
    
    handleGroupedTopology(topology) {
        // Convert groups to nodes format for visualization
        const groups = topology.groups || [];
        this.edges = topology.edges || [];
        
        this.nodes = groups.map(group => ({
            id: group.id,
            type: group.type.replace('_group', ''), // 'sender' or 'receiver'
            isGroup: true,
            ip: group.ip,
            status: group.status,
            client_count: group.client_count,
            port_count: group.port_count,
            ports: group.ports || [],
            packets_sent: group.packets_sent,
            bytes_sent: group.bytes_sent,
            packets_received: group.packets_received,
            bytes_received: group.bytes_received,
            encryption_enabled: group.encryption_enabled,
            decode_success_count: group.decode_success_count,
            decode_failure_count: group.decode_failure_count,
            avg_latency_ms: group.avg_latency_ms,
            last_seen: group.last_seen,
            destination_ip: group.destination_ip,
            detected_source_ip: group.detected_source_ip
        }));
        
        // Build node map
        this.nodeMap.clear();
        this.nodes.forEach(node => this.nodeMap.set(node.id, node));
        
        this.updateVisualization();
        this.updateSidebar();
        this.updateSummary();
    }
    
    setupThemeToggle() {
        const toggle = document.getElementById('themeToggle');
        const savedTheme = localStorage.getItem('theme') || 'dark';
        
        if (savedTheme === 'light') {
            document.documentElement.setAttribute('data-theme', 'light');
            toggle.textContent = '☀️';
        }
        
        toggle.addEventListener('click', () => {
            const current = document.documentElement.getAttribute('data-theme');
            const newTheme = current === 'light' ? 'dark' : 'light';
            document.documentElement.setAttribute('data-theme', newTheme);
            localStorage.setItem('theme', newTheme);
            toggle.textContent = newTheme === 'light' ? '☀️' : '🌙';
        });
    }
    
    setupSVG() {
        const container = document.querySelector('.network-container');
        this.width = container.clientWidth;
        this.height = container.clientHeight;
        
        this.svg = d3.select('#networkGraph')
            .attr('width', this.width)
            .attr('height', this.height);
        
        // Add arrow marker for directed edges
        this.svg.append('defs').append('marker')
            .attr('id', 'arrowhead')
            .attr('viewBox', '-0 -5 10 10')
            .attr('refX', 35)
            .attr('refY', 0)
            .attr('orient', 'auto')
            .attr('markerWidth', 8)
            .attr('markerHeight', 8)
            .append('path')
            .attr('d', 'M 0,-5 L 10,0 L 0,5')
            .attr('fill', 'var(--text-secondary)');
        
        // Create groups for links and nodes
        this.svg.append('g').attr('class', 'links');
        this.svg.append('g').attr('class', 'link-labels');
        this.svg.append('g').attr('class', 'nodes');
        
        // Initialize force simulation
        this.simulation = d3.forceSimulation()
            .force('link', d3.forceLink().id(d => d.id).distance(200))
            .force('charge', d3.forceManyBody().strength(-400))
            .force('center', d3.forceCenter(this.width / 2, this.height / 2))
            .force('collision', d3.forceCollide().radius(50))
            .on('tick', () => this.tick());
    }
    
    handleResize() {
        const container = document.querySelector('.network-container');
        this.width = container.clientWidth;
        this.height = container.clientHeight;
        
        this.svg
            .attr('width', this.width)
            .attr('height', this.height);
        
        this.simulation.force('center', d3.forceCenter(this.width / 2, this.height / 2));
        this.simulation.alpha(0.3).restart();
    }
    
    connectWebSocket() {
        const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
        const wsUrl = `${protocol}//${window.location.host}/ws`;
        
        this.ws = new WebSocket(wsUrl);
        
        this.ws.onopen = () => {
            console.log('WebSocket connected');
            this.updateConnectionStatus(true);
            this.wsReconnectDelay = 1000;
        };
        
        this.ws.onclose = () => {
            console.log('WebSocket disconnected');
            this.updateConnectionStatus(false);
            
            // Reconnect with exponential backoff
            setTimeout(() => {
                this.connectWebSocket();
            }, this.wsReconnectDelay);
            
            this.wsReconnectDelay = Math.min(this.wsReconnectDelay * 2, 30000);
        };
        
        this.ws.onerror = (error) => {
            console.error('WebSocket error:', error);
        };
        
        this.ws.onmessage = (event) => {
            const message = JSON.parse(event.data);
            this.handleMessage(message);
        };
    }
    
    updateConnectionStatus(connected) {
        const indicator = document.getElementById('wsStatus');
        const text = document.getElementById('wsStatusText');
        
        if (connected) {
            indicator.classList.add('connected');
            text.textContent = 'Connected';
        } else {
            indicator.classList.remove('connected');
            text.textContent = 'Disconnected';
        }
    }
    
    handleMessage(message) {
        console.log('Received:', message.type, message.data);
        
        switch (message.type) {
            case 'full_topology':
                this.handleFullTopology(message.data);
                break;
            case 'node_update':
                this.handleNodeUpdate(message.data);
                break;
            case 'node_offline':
                this.handleNodeOffline(message.data);
                break;
            case 'edge_update':
                this.handleEdgeUpdate(message.data);
                break;
        }
    }
    
    handleFullTopology(topology) {
        this.nodes = topology.nodes || [];
        this.edges = topology.edges || [];
        
        // Build node map for quick lookups
        this.nodeMap.clear();
        this.nodes.forEach(node => this.nodeMap.set(node.id, node));
        
        this.updateVisualization();
        this.updateSidebar();
        this.updateSummary();
    }
    
    handleNodeUpdate(node) {
        const existingNode = this.nodeMap.get(node.id);
        
        if (existingNode) {
            // Update existing node
            Object.assign(existingNode, node);
        } else {
            // Add new node
            node.x = this.width / 2 + (Math.random() - 0.5) * 100;
            node.y = this.height / 2 + (Math.random() - 0.5) * 100;
            this.nodes.push(node);
            this.nodeMap.set(node.id, node);
        }
        
        this.updateVisualization();
        this.updateSidebar();
        this.updateSummary();
        
        // Pulse animation for updated node
        this.pulseNode(node.id);
    }
    
    handleNodeOffline(data) {
        const node = this.nodeMap.get(data.id);
        if (node) {
            node.status = 'offline';
            this.updateVisualization();
            this.updateSidebar();
        }
    }
    
    handleEdgeUpdate(edge) {
        const existingIndex = this.edges.findIndex(e => 
            e.from === edge.from && e.to === edge.to
        );
        
        if (existingIndex >= 0) {
            this.edges[existingIndex] = edge;
        } else {
            this.edges.push(edge);
        }
        
        this.updateVisualization();
        this.updateSummary();
    }
    
    updateVisualization() {
        // Show/hide empty state
        const emptyState = document.getElementById('emptyState');
        const connectionInfo = document.getElementById('connectionInfo');
        
        if (this.nodes.length === 0) {
            emptyState.style.display = 'flex';
            connectionInfo.style.display = 'none';
            return;
        } else {
            emptyState.style.display = 'none';
            connectionInfo.style.display = 'block';
        }
        
        // Transform edges to use node references
        const links = this.edges.map(e => ({
            source: e.from,
            target: e.to,
            ...e
        }));
        
        // Update links
        const linkGroup = this.svg.select('.links');
        const link = linkGroup.selectAll('.link')
            .data(links, d => `${d.from}-${d.to}`);
        
        link.exit().remove();
        
        const linkEnter = link.enter()
            .append('line')
            .attr('class', d => `link ${d.is_mirror ? 'mirror' : ''} ${d.packets_received > 0 ? 'active' : ''}`)
            .attr('stroke-width', d => Math.max(2, Math.min(8, d.packets_received / 1000)))
            .attr('marker-end', 'url(#arrowhead)');
        
        link.merge(linkEnter)
            .attr('class', d => `link ${d.is_mirror ? 'mirror' : ''} ${d.packets_received > 0 ? 'active' : ''}`)
            .attr('stroke-width', d => Math.max(2, Math.min(8, d.packets_received / 1000)));
        
        // Update link labels
        const labelGroup = this.svg.select('.link-labels');
        const linkLabel = labelGroup.selectAll('.link-label')
            .data(links, d => `${d.from}-${d.to}`);
        
        linkLabel.exit().remove();
        
        const linkLabelEnter = linkLabel.enter()
            .append('text')
            .attr('class', 'link-label');
        
        linkLabel.merge(linkLabelEnter)
            .text(d => {
                const latency = d.avg_latency_ms > 0 ? `${d.avg_latency_ms.toFixed(1)}ms` : '-';
                const pps = this.formatNumber(d.packets_received);
                return `${pps} pkts • ${latency}`;
            });
        
        // Update nodes
        const nodeGroup = this.svg.select('.nodes');
        const node = nodeGroup.selectAll('.node')
            .data(this.nodes, d => d.id);
        
        node.exit().remove();
        
        const nodeEnter = node.enter()
            .append('g')
            .attr('class', 'node')
            .call(d3.drag()
                .on('start', (event, d) => this.dragStarted(event, d))
                .on('drag', (event, d) => this.dragged(event, d))
                .on('end', (event, d) => this.dragEnded(event, d)))
            .on('click', (event, d) => this.selectNode(d))
            .on('mouseover', (event, d) => this.showTooltip(event, d))
            .on('mouseout', () => this.hideTooltip());
        
        nodeEnter.append('circle')
            .attr('class', d => `node-circle ${d.type} ${d.status === 'offline' ? 'offline' : ''}`)
            .attr('r', 25);
        
        nodeEnter.append('text')
            .attr('class', 'node-label')
            .attr('dy', -35)
            .text(d => this.getNodeLabel(d));
        
        nodeEnter.append('text')
            .attr('class', 'node-sublabel')
            .attr('dy', 40)
            .text(d => this.getNodeSublabel(d));
        
        // Update existing nodes
        const nodeUpdate = node.merge(nodeEnter);
        
        nodeUpdate.select('.node-circle')
            .attr('class', d => `node-circle ${d.type} ${d.status === 'offline' ? 'offline' : ''} ${d.isGroup ? 'group' : ''}`)
            .attr('r', d => d.isGroup ? 30 : 25);
        
        nodeUpdate.select('.node-label')
            .text(d => this.getNodeLabel(d));
        
        nodeUpdate.select('.node-sublabel')
            .text(d => this.getNodeSublabel(d));
        
        // Update simulation
        this.simulation.nodes(this.nodes);
        this.simulation.force('link').links(links);
        this.simulation.alpha(0.3).restart();
    }
    
    tick() {
        // Update link positions
        this.svg.select('.links').selectAll('.link')
            .attr('x1', d => d.source.x)
            .attr('y1', d => d.source.y)
            .attr('x2', d => d.target.x)
            .attr('y2', d => d.target.y);
        
        // Update link label positions
        this.svg.select('.link-labels').selectAll('.link-label')
            .attr('x', d => (d.source.x + d.target.x) / 2)
            .attr('y', d => (d.source.y + d.target.y) / 2 - 10);
        
        // Update node positions
        this.svg.select('.nodes').selectAll('.node')
            .attr('transform', d => `translate(${d.x}, ${d.y})`);
    }
    
    dragStarted(event, d) {
        if (!event.active) this.simulation.alphaTarget(0.3).restart();
        d.fx = d.x;
        d.fy = d.y;
    }
    
    dragged(event, d) {
        d.fx = event.x;
        d.fy = event.y;
    }
    
    dragEnded(event, d) {
        if (!event.active) this.simulation.alphaTarget(0);
        d.fx = null;
        d.fy = null;
    }
    
    pulseNode(nodeId) {
        this.svg.select('.nodes').selectAll('.node')
            .filter(d => d.id === nodeId)
            .select('circle')
            .classed('node-pulse', true)
            .transition()
            .duration(500)
            .on('end', function() {
                d3.select(this).classed('node-pulse', false);
            });
    }
    
    selectNode(node) {
        this.selectedNode = node;
        
        // Update sidebar selection
        document.querySelectorAll('.node-card').forEach(card => {
            card.classList.toggle('selected', card.dataset.nodeId === node.id);
        });
    }
    
    getNodeLabel(node) {
        if (node.isGroup) {
            return `${node.ip} (${node.port_count} ports)`;
        }
        return node.id;
    }
    
    getNodeSublabel(node) {
        if (node.isGroup) {
            const packets = node.type === 'sender' ? node.packets_sent : node.packets_received;
            return `${this.formatNumber(packets)} pkts`;
        }
        return node.type === 'sender' ? node.source_ip : node.listen_ip;
    }
    
    showTooltip(event, node) {
        const tooltip = document.getElementById('tooltip');
        
        let content = '';
        
        // Handle grouped nodes
        if (node.isGroup) {
            content = this.buildGroupTooltip(node);
        } else {
            content = this.buildIndividualTooltip(node);
        }
        
        tooltip.innerHTML = content;
        tooltip.style.left = (event.pageX + 15) + 'px';
        tooltip.style.top = (event.pageY + 15) + 'px';
        tooltip.classList.add('visible');
    }
    
    buildGroupTooltip(node) {
        let content = `
            <div class="tooltip-title">
                ${node.ip}
                <span class="node-type-badge ${node.type}">${node.type} group</span>
            </div>
            <div class="tooltip-row">
                <span class="tooltip-label">Clients:</span>
                <span>${node.client_count}</span>
            </div>
            <div class="tooltip-row">
                <span class="tooltip-label">Total Ports:</span>
                <span>${node.port_count}</span>
            </div>
        `;
        
        if (node.type === 'sender') {
            content += `
                <div class="tooltip-row">
                    <span class="tooltip-label">Packets Sent:</span>
                    <span>${this.formatNumber(node.packets_sent)}</span>
                </div>
                <div class="tooltip-row">
                    <span class="tooltip-label">Bytes Sent:</span>
                    <span>${this.formatBytes(node.bytes_sent)}</span>
                </div>
            `;
        } else {
            content += `
                <div class="tooltip-row">
                    <span class="tooltip-label">Packets Received:</span>
                    <span>${this.formatNumber(node.packets_received)}</span>
                </div>
                <div class="tooltip-row">
                    <span class="tooltip-label">Avg Latency:</span>
                    <span>${node.avg_latency_ms ? node.avg_latency_ms.toFixed(2) + 'ms' : '-'}</span>
                </div>
            `;
        }
        
        content += `
            <div class="tooltip-row">
                <span class="tooltip-label">Encryption:</span>
                <span>${node.encryption_enabled ? '🔒 Enabled' : '🔓 Disabled'}</span>
            </div>
            <div class="tooltip-row">
                <span class="tooltip-label">Status:</span>
                <span>${node.status}</span>
            </div>
        `;
        
        // Show top ports
        if (node.ports && node.ports.length > 0) {
            content += `<div class="tooltip-divider"></div>`;
            content += `<div class="tooltip-subtitle">Top ${Math.min(node.ports.length, 10)} Ports:</div>`;
            content += `<div class="tooltip-ports">`;
            
            node.ports.slice(0, 10).forEach(port => {
                const packets = node.type === 'sender' ? port.packets_sent : port.packets_received;
                const statusDot = port.status === 'online' ? '🟢' : '⚪';
                const latency = port.avg_latency_ms ? ` • ${port.avg_latency_ms.toFixed(1)}ms` : '';
                content += `
                    <div class="tooltip-port-row">
                        <span>${statusDot} Port ${port.port}</span>
                        <span>${this.formatNumber(packets)} pkts${latency}</span>
                    </div>
                `;
            });
            
            content += `</div>`;
        }
        
        return content;
    }
    
    buildIndividualTooltip(node) {
        let content = `
            <div class="tooltip-title">
                ${node.id}
                <span class="node-type-badge ${node.type}">${node.type}</span>
            </div>
        `;
        
        if (node.type === 'sender') {
            content += `
                <div class="tooltip-row">
                    <span class="tooltip-label">Source IP:</span>
                    <span>${node.source_ip || '-'}</span>
                </div>
                <div class="tooltip-row">
                    <span class="tooltip-label">Destination:</span>
                    <span>${node.destination_ip}:${node.destination_port}</span>
                </div>
                <div class="tooltip-row">
                    <span class="tooltip-label">Packets Sent:</span>
                    <span>${this.formatNumber(node.packets_sent)}</span>
                </div>
                <div class="tooltip-row">
                    <span class="tooltip-label">Bytes Sent:</span>
                    <span>${this.formatBytes(node.bytes_sent)}</span>
                </div>
            `;
        } else {
            content += `
                <div class="tooltip-row">
                    <span class="tooltip-label">Listen:</span>
                    <span>${node.listen_ip}:${node.listen_port}</span>
                </div>
                <div class="tooltip-row">
                    <span class="tooltip-label">Source Detected:</span>
                    <span>${node.detected_source_ip || '-'}</span>
                </div>
                <div class="tooltip-row">
                    <span class="tooltip-label">Packets Received:</span>
                    <span>${this.formatNumber(node.packets_received)}</span>
                </div>
                <div class="tooltip-row">
                    <span class="tooltip-label">Avg Latency:</span>
                    <span>${node.avg_latency_ms ? node.avg_latency_ms.toFixed(2) + 'ms' : '-'}</span>
                </div>
            `;
        }
        
        content += `
            <div class="tooltip-row">
                <span class="tooltip-label">Encryption:</span>
                <span>${node.encryption_enabled ? '🔒 Enabled' : '🔓 Disabled'}</span>
            </div>
            <div class="tooltip-row">
                <span class="tooltip-label">Status:</span>
                <span>${node.status}</span>
            </div>
        `;
        
        return content;
    }
    
    hideTooltip() {
        const tooltip = document.getElementById('tooltip');
        tooltip.classList.remove('visible');
    }
    
    updateSidebar() {
        const nodeList = document.getElementById('nodeList');
        nodeList.innerHTML = '';
        
        // Sort: senders first, then receivers
        const sortedNodes = [...this.nodes].sort((a, b) => {
            if (a.type !== b.type) return a.type === 'sender' ? -1 : 1;
            return a.id.localeCompare(b.id);
        });
        
        sortedNodes.forEach(node => {
            const card = document.createElement('div');
            card.className = `node-card ${this.selectedNode?.id === node.id ? 'selected' : ''} ${node.isGroup ? 'group-card' : ''}`;
            card.dataset.nodeId = node.id;
            card.onclick = () => this.selectNode(node);
            
            let metricsHTML = '';
            let displayName = node.id;
            let typeBadge = node.type;
            
            // Handle grouped nodes
            if (node.isGroup) {
                displayName = node.ip;
                typeBadge = `${node.type} (${node.port_count} ports)`;
                
                if (node.type === 'sender') {
                    metricsHTML = `
                        <div class="metric">
                            <span class="metric-label">Packets</span>
                            <span class="metric-value">${this.formatNumber(node.packets_sent)}</span>
                        </div>
                        <div class="metric">
                            <span class="metric-label">Bytes</span>
                            <span class="metric-value">${this.formatBytes(node.bytes_sent)}</span>
                        </div>
                        <div class="metric">
                            <span class="metric-label">Ports</span>
                            <span class="metric-value">${node.port_count}</span>
                        </div>
                        <div class="metric">
                            <span class="metric-label">Encryption</span>
                            <span class="metric-value">${node.encryption_enabled ? '🔒' : '🔓'}</span>
                        </div>
                    `;
                } else {
                    metricsHTML = `
                        <div class="metric">
                            <span class="metric-label">Packets</span>
                            <span class="metric-value">${this.formatNumber(node.packets_received)}</span>
                        </div>
                        <div class="metric">
                            <span class="metric-label">Latency</span>
                            <span class="metric-value">${node.avg_latency_ms ? node.avg_latency_ms.toFixed(1) + 'ms' : '-'}</span>
                        </div>
                        <div class="metric">
                            <span class="metric-label">Ports</span>
                            <span class="metric-value">${node.port_count}</span>
                        </div>
                        <div class="metric">
                            <span class="metric-label">Encryption</span>
                            <span class="metric-value">${node.encryption_enabled ? '🔒' : '🔓'}</span>
                        </div>
                    `;
                }
            } else {
                // Individual nodes
                if (node.type === 'sender') {
                    metricsHTML = `
                        <div class="metric">
                            <span class="metric-label">Packets</span>
                            <span class="metric-value">${this.formatNumber(node.packets_sent)}</span>
                        </div>
                        <div class="metric">
                            <span class="metric-label">Bytes</span>
                            <span class="metric-value">${this.formatBytes(node.bytes_sent)}</span>
                        </div>
                        <div class="metric">
                            <span class="metric-label">Target</span>
                            <span class="metric-value">${node.destination_ip || '-'}</span>
                        </div>
                        <div class="metric">
                            <span class="metric-label">Encryption</span>
                            <span class="metric-value">${node.encryption_enabled ? '🔒' : '🔓'}</span>
                        </div>
                    `;
                } else {
                    metricsHTML = `
                        <div class="metric">
                            <span class="metric-label">Packets</span>
                            <span class="metric-value">${this.formatNumber(node.packets_received)}</span>
                        </div>
                        <div class="metric">
                            <span class="metric-label">Latency</span>
                            <span class="metric-value">${node.avg_latency_ms ? node.avg_latency_ms.toFixed(1) + 'ms' : '-'}</span>
                        </div>
                        <div class="metric">
                            <span class="metric-label">Source</span>
                            <span class="metric-value">${node.detected_source_ip || '-'}</span>
                        </div>
                        <div class="metric">
                            <span class="metric-label">Encryption</span>
                            <span class="metric-value">${node.encryption_enabled ? '🔒' : '🔓'}</span>
                        </div>
                    `;
                }
            }
            
            card.innerHTML = `
                <div class="node-card-header">
                    <div class="node-name">
                        ${displayName}
                        <span class="node-type-badge ${node.type}">${typeBadge}</span>
                    </div>
                    <div class="node-status">
                        <div class="node-status-dot ${node.status}"></div>
                        ${node.status}
                    </div>
                </div>
                <div class="node-metrics">
                    ${metricsHTML}
                </div>
            `;
            
            nodeList.appendChild(card);
        });
    }
    
    updateSummary() {
        document.getElementById('totalNodes').textContent = this.nodes.length;
        document.getElementById('totalEdges').textContent = this.edges.length;
        
        // Calculate average latency
        const latencies = this.edges
            .filter(e => e.avg_latency_ms > 0)
            .map(e => e.avg_latency_ms);
        
        if (latencies.length > 0) {
            const avgLatency = latencies.reduce((a, b) => a + b, 0) / latencies.length;
            document.getElementById('avgLatency').textContent = avgLatency.toFixed(1) + 'ms';
        } else {
            document.getElementById('avgLatency').textContent = '-';
        }
    }
    
    formatNumber(num) {
        if (num === undefined || num === null) return '0';
        if (num >= 1000000) return (num / 1000000).toFixed(1) + 'M';
        if (num >= 1000) return (num / 1000).toFixed(1) + 'K';
        return num.toString();
    }
    
    formatBytes(bytes) {
        if (bytes === undefined || bytes === null) return '0 B';
        if (bytes >= 1073741824) return (bytes / 1073741824).toFixed(1) + ' GB';
        if (bytes >= 1048576) return (bytes / 1048576).toFixed(1) + ' MB';
        if (bytes >= 1024) return (bytes / 1024).toFixed(1) + ' KB';
        return bytes + ' B';
    }
}

// Initialize dashboard when DOM is loaded
document.addEventListener('DOMContentLoaded', () => {
    window.dashboard = new NetworkDashboard();
});
