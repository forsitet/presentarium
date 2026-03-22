export type MessageHandler = (data: unknown) => void

class WSSocket {
  private ws: WebSocket | null = null
  private handlers: Map<string, MessageHandler[]> = new Map()
  private reconnectAttempts = 0
  private maxReconnects = 5
  private reconnectDelay = 1000
  private roomCode = ''
  private token = ''
  private name = ''
  private reconnectTimer: ReturnType<typeof setTimeout> | null = null

  connect(roomCode: string, token?: string, name?: string) {
    // Close any existing connection before creating a new one.
    this._closeExisting()
    this.roomCode = roomCode
    this.token = token || ''
    this.name = name || ''
    this.reconnectAttempts = 0
    this.maxReconnects = 5
    this._connect()
  }

  /** Close the current WebSocket without triggering reconnection. */
  private _closeExisting() {
    if (this.reconnectTimer) {
      clearTimeout(this.reconnectTimer)
      this.reconnectTimer = null
    }
    if (this.ws) {
      // Remove onclose to prevent reconnection from the old socket.
      this.ws.onclose = null
      this.ws.onmessage = null
      if (
        this.ws.readyState === WebSocket.OPEN ||
        this.ws.readyState === WebSocket.CONNECTING
      ) {
        this.ws.close()
      }
      this.ws = null
    }
  }

  private _connect() {
    const proto = window.location.protocol === 'https:' ? 'wss' : 'ws'
    const host = window.location.host
    const sessionToken = localStorage.getItem(`session_token_${this.roomCode}`) || ''
    const params = new URLSearchParams()
    if (this.token) params.set('token', this.token)
    if (this.name) params.set('name', this.name)
    if (sessionToken) params.set('session_token', sessionToken)
    this.ws = new WebSocket(`${proto}://${host}/ws/room/${this.roomCode}?${params}`)

    this.ws.onmessage = (event) => {
      try {
        const msg = JSON.parse(event.data) as { type: string; data: unknown }
        if (msg.type === 'connected') {
          const data = msg.data as { session_token?: string }
          if (data?.session_token) {
            localStorage.setItem(`session_token_${this.roomCode}`, data.session_token)
          }
        }
        const handlers = this.handlers.get(msg.type) || []
        handlers.forEach((h) => h(msg.data))
        const allHandlers = this.handlers.get('*') || []
        allHandlers.forEach((h) => h(msg))
      } catch {
        // ignore parse errors
      }
    }

    this.ws.onclose = () => {
      if (this.reconnectAttempts < this.maxReconnects) {
        const delay = this.reconnectDelay * Math.pow(2, this.reconnectAttempts)
        this.reconnectAttempts++
        this.reconnectTimer = setTimeout(() => {
          this.reconnectTimer = null
          this._connect()
        }, delay)
      }
    }
  }

  on(type: string, handler: MessageHandler) {
    if (!this.handlers.has(type)) this.handlers.set(type, [])
    this.handlers.get(type)!.push(handler)
  }

  off(type: string, handler: MessageHandler) {
    const handlers = this.handlers.get(type) || []
    this.handlers.set(type, handlers.filter((h) => h !== handler))
  }

  send(type: string, data: unknown) {
    if (this.ws?.readyState === WebSocket.OPEN) {
      this.ws.send(JSON.stringify({ type, data }))
    }
  }

  disconnect() {
    this.maxReconnects = 0
    this._closeExisting()
  }
}

export const socket = new WSSocket()
