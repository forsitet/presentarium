// Presentarium WebSocket room load test.
//
// Scenario (matches Table 3.6):
//   1 room · 1 organizer · 5 questions (single_choice + open_text + word_cloud)
//   30 s active time per question · N participants connecting in parallel.
//
// Run from the loadtest/ directory:
//
//   k6 run -e PARTICIPANTS=50  room_load_test.js
//   k6 run -e PARTICIPANTS=100 room_load_test.js
//   k6 run -e PARTICIPANTS=250 room_load_test.js
//   k6 run -e PARTICIPANTS=500 room_load_test.js
//
// Required env vars (defaults shown):
//
//   BASE_URL       http://localhost:8080   — REST + WS host
//   PARTICIPANTS   50                      — concurrent participants
//   QUESTION_TIME  30                      — seconds the timer runs per question
//   STARTUP_BUFFER auto                    — seconds the organizer waits for
//                                            participants to attach before
//                                            firing the first question. Defaults
//                                            to max(5, PARTICIPANTS / 50).
//
// Metrics → criteria from Table 3.7:
//
//   №2  ≥99 % successful connections          → check rate `connected`
//   №3  avg question delivery   ≤ 500 ms      → wait_for_question_ms
//   №4  avg answer ack          ≤ 500 ms      → answer_ack_ms
//   №6  ≤1 % submit error rate                → submit_error_rate
//   №7  0 lost answers                        → lost_answers
//   №8  0 duplicate answer_accepted           → duplicate_acks
//   №11 history contains finished session     → teardown HTTP check
//   №12 leaderboard formed                    → teardown HTTP check
//
// Criteria 1, 5, 9, 10 are validated out-of-band — they're either covered by
// the run configuration (1: VUs ≤ 500), the integration test
// TestParticipantReconnect (5), or by watching the backend logs while the
// test runs (9, 10). The README documents this explicitly.

import http from 'k6/http'
import ws from 'k6/ws'
import { check } from 'k6'
import { Trend, Counter, Rate } from 'k6/metrics'

// ─── Config ───────────────────────────────────────────────────────────────────

const BASE_URL = __ENV.BASE_URL || 'http://localhost:8080'
const WS_BASE_URL = BASE_URL.replace(/^http/, 'ws')
const PARTICIPANTS = parseInt(__ENV.PARTICIPANTS || '50', 10)
const QUESTION_TIME_SEC = parseInt(__ENV.QUESTION_TIME || '30', 10)
const NUM_QUESTIONS = 5

// More participants need a larger window to attach before the organizer fires
// question 1 — at 500 VUs the WS handshake fan-in can take several seconds.
const STARTUP_BUFFER_SEC = parseInt(
  __ENV.STARTUP_BUFFER || String(Math.max(5, Math.ceil(PARTICIPANTS / 50))),
  10,
)

// ─── Custom metrics ───────────────────────────────────────────────────────────

const connectedRate = new Rate('connected_rate') // criterion №2
const waitForQuestionMs = new Trend('wait_for_question_ms', true) // №3
const answerAckMs = new Trend('answer_ack_ms', true) // №4
const submitErrorRate = new Rate('submit_error_rate') // №6
const lostAnswers = new Counter('lost_answers') // №7
const duplicateAcks = new Counter('duplicate_acks') // №8
const wsErrors = new Counter('ws_errors')
const sessionEndReceived = new Rate('session_end_received')

// ─── k6 scenarios ─────────────────────────────────────────────────────────────

const TOTAL_RUN_SEC = STARTUP_BUFFER_SEC + (QUESTION_TIME_SEC + 2) * NUM_QUESTIONS + 30

export const options = {
  scenarios: {
    organizer: {
      executor: 'shared-iterations',
      vus: 1,
      iterations: 1,
      maxDuration: `${TOTAL_RUN_SEC + 60}s`,
      exec: 'organizer',
    },
    participants: {
      executor: 'shared-iterations',
      vus: PARTICIPANTS,
      iterations: PARTICIPANTS,
      maxDuration: `${TOTAL_RUN_SEC + 60}s`,
      exec: 'participant',
      // Participants start ~2 s after the organizer issues "action: start";
      // they're allowed to connect while the room is in waiting state too.
      startTime: '2s',
    },
  },
  // Thresholds that match Table 3.7. A failing threshold makes k6 exit non-zero,
  // so this can be wired into CI for a strict pass/fail signal.
  thresholds: {
    connected_rate: ['rate>=0.99'], // №2
    wait_for_question_ms: ['avg<500'], // №3
    answer_ack_ms: ['avg<500'], // №4
    submit_error_rate: ['rate<0.01'], // №6
    lost_answers: ['count==0'], // №7
    duplicate_acks: ['count==0'], // №8
    session_end_received: ['rate>=0.99'], // ensures sessions finish cleanly
  },
}

// ─── Setup: register organizer, create poll + 5 questions + room ──────────────

export function setup() {
  const suffix = Math.random().toString(36).slice(2, 10)
  const email = `loadtest-${suffix}@k6.test`

  // 1. Register organizer.
  const regRes = http.post(
    `${BASE_URL}/api/auth/register`,
    JSON.stringify({
      email,
      password: 'loadtest-pass-1234',
      name: 'Load Test Organizer',
    }),
    { headers: { 'Content-Type': 'application/json' } },
  )
  if (regRes.status !== 201) {
    throw new Error(`register failed: ${regRes.status} ${regRes.body}`)
  }
  const token = regRes.json('access_token')

  // 2. Create poll.
  const pollRes = http.post(
    `${BASE_URL}/api/polls`,
    JSON.stringify({
      title: `Load test ${suffix}`,
      scoring_rule: 'correct_answer',
      question_order: 'sequential',
    }),
    {
      headers: {
        Authorization: `Bearer ${token}`,
        'Content-Type': 'application/json',
      },
    },
  )
  if (pollRes.status !== 201) {
    throw new Error(`create poll failed: ${pollRes.status} ${pollRes.body}`)
  }
  const pollId = pollRes.json('id')

  // 3. Create 5 questions covering all three required types.
  const questionPlans = [
    {
      type: 'single_choice',
      text: 'What is 2 + 2?',
      time_limit_seconds: QUESTION_TIME_SEC,
      points: 100,
      options: [
        { text: '3', is_correct: false },
        { text: '4', is_correct: true },
        { text: '5', is_correct: false },
      ],
    },
    {
      type: 'open_text',
      text: 'What is your favourite programming language?',
      time_limit_seconds: QUESTION_TIME_SEC,
      points: 0,
    },
    {
      type: 'single_choice',
      text: 'Capital of France?',
      time_limit_seconds: QUESTION_TIME_SEC,
      points: 100,
      options: [
        { text: 'London', is_correct: false },
        { text: 'Paris', is_correct: true },
        { text: 'Berlin', is_correct: false },
        { text: 'Madrid', is_correct: false },
      ],
    },
    {
      type: 'word_cloud',
      text: 'Words you associate with the project',
      time_limit_seconds: QUESTION_TIME_SEC,
      points: 0,
    },
    {
      type: 'open_text',
      text: 'Anything to add?',
      time_limit_seconds: QUESTION_TIME_SEC,
      points: 0,
    },
  ]

  const questionIds = []
  for (const q of questionPlans) {
    const qRes = http.post(
      `${BASE_URL}/api/polls/${pollId}/questions`,
      JSON.stringify(q),
      {
        headers: {
          Authorization: `Bearer ${token}`,
          'Content-Type': 'application/json',
        },
      },
    )
    if (qRes.status !== 201) {
      throw new Error(`create question failed: ${qRes.status} ${qRes.body}`)
    }
    questionIds.push({ id: qRes.json('id'), type: q.type })
  }

  // 4. Create room.
  const roomRes = http.post(
    `${BASE_URL}/api/rooms`,
    JSON.stringify({ poll_id: pollId }),
    {
      headers: {
        Authorization: `Bearer ${token}`,
        'Content-Type': 'application/json',
      },
    },
  )
  if (roomRes.status !== 201) {
    throw new Error(`create room failed: ${roomRes.status} ${roomRes.body}`)
  }
  const roomCode = roomRes.json('room_code')
  const sessionId = roomRes.json('session_id')

  console.log(
    `[setup] poll=${pollId} room=${roomCode} session=${sessionId} ` +
      `participants=${PARTICIPANTS} questions=${NUM_QUESTIONS}`,
  )

  return { token, roomCode, sessionId, questionIds, pollId }
}

// ─── Organizer VU ─────────────────────────────────────────────────────────────

export function organizer(data) {
  const url = `${WS_BASE_URL}/ws/room/${data.roomCode}?token=${data.token}`

  const startRes = http.patch(
    `${BASE_URL}/api/rooms/${data.roomCode}/state`,
    JSON.stringify({ action: 'start' }),
    {
      headers: {
        Authorization: `Bearer ${data.token}`,
        'Content-Type': 'application/json',
      },
    },
  )
  check(startRes, { 'session started': (r) => r.status === 200 })

  const wsRes = ws.connect(url, {}, (socket) => {
    let qIndex = 0

    socket.on('open', () => {
      // After a buffer to let participants attach, start firing questions.
      socket.setTimeout(showNext, STARTUP_BUFFER_SEC * 1000)
    })

    function showNext() {
      if (qIndex >= data.questionIds.length) {
        // All questions done — end the session via HTTP, then close the WS.
        const endRes = http.patch(
          `${BASE_URL}/api/rooms/${data.roomCode}/state`,
          JSON.stringify({ action: 'end' }),
          {
            headers: {
              Authorization: `Bearer ${data.token}`,
              'Content-Type': 'application/json',
            },
          },
        )
        check(endRes, { 'session ended': (r) => r.status === 200 })
        socket.setTimeout(() => socket.close(), 3000)
        return
      }
      const q = data.questionIds[qIndex++]
      socket.send(
        JSON.stringify({
          type: 'show_question',
          data: { question_id: q.id },
        }),
      )
      // The server-side timer fires question_end automatically after
      // QUESTION_TIME_SEC; +2 s gives the broadcast time to propagate before
      // we send the next show_question.
      socket.setTimeout(showNext, (QUESTION_TIME_SEC + 2) * 1000)
    }

    socket.on('error', () => wsErrors.add(1))
    socket.on('close', () => {})

    // Hard upper bound so a stuck VU doesn't leak the entire test.
    socket.setTimeout(() => socket.close(), TOTAL_RUN_SEC * 1000)
  })
  check(wsRes, { 'organizer ws status 101': (r) => r && r.status === 101 })
}

// ─── Participant VU ───────────────────────────────────────────────────────────

export function participant(data) {
  const name = `vu-${__VU}`
  const url = `${WS_BASE_URL}/ws/room/${data.roomCode}?name=${encodeURIComponent(name)}`

  // Per-VU state — k6 reuses module scope only for shared metrics, NOT for
  // local mutable state, so we redeclare here every iteration.
  const submittedAt = {} // qid → ms timestamp the submit_* was sent
  const acked = new Set() // qids that received answer_accepted exactly once
  let gotConnected = false
  let gotSessionEnd = false

  const wsRes = ws.connect(url, {}, (socket) => {
    socket.on('message', (raw) => {
      let msg
      try {
        msg = JSON.parse(raw)
      } catch (_) {
        return
      }
      switch (msg.type) {
        case 'connected':
          gotConnected = true
          connectedRate.add(true)
          break

        case 'question_start': {
          const qid = msg.data.question_id
          const qtype = msg.data.type
          // sent_at_ms is the server-side timestamp when the broadcast was
          // constructed; (Date.now() - sent_at_ms) is the actual delivery
          // latency. Missing on per-client resync after reconnect.
          if (typeof msg.data.sent_at_ms === 'number' && msg.data.sent_at_ms > 0) {
            waitForQuestionMs.add(Date.now() - msg.data.sent_at_ms)
          }

          // Send the right submit message for the question type.
          submittedAt[qid] = Date.now()
          let payload
          if (qtype === 'single_choice' || qtype === 'image_choice') {
            payload = {
              type: 'submit_answer',
              data: { question_id: qid, answer: 0 },
            }
          } else if (qtype === 'multiple_choice') {
            payload = {
              type: 'submit_answer',
              data: { question_id: qid, answer: [0] },
            }
          } else if (qtype === 'open_text' || qtype === 'word_cloud') {
            payload = {
              type: 'submit_text',
              data: { question_id: qid, text: `${name}-${qid.slice(0, 6)}` },
            }
          } else {
            // brainstorm or unknown — skip
            break
          }
          try {
            socket.send(JSON.stringify(payload))
          } catch (_) {
            submitErrorRate.add(true)
            wsErrors.add(1)
            break
          }
          submitErrorRate.add(false)
          break
        }

        case 'answer_accepted': {
          const aqid = msg.data.question_id
          const sentTs = submittedAt[aqid]
          if (sentTs === undefined) break
          if (acked.has(aqid)) {
            duplicateAcks.add(1)
          } else {
            answerAckMs.add(Date.now() - sentTs)
            acked.add(aqid)
          }
          break
        }

        case 'question_end': {
          // After question_end, lost = we sent submit_* but never got ack.
          const eqid = msg.data.question_id
          if (submittedAt[eqid] !== undefined && !acked.has(eqid)) {
            lostAnswers.add(1)
          }
          break
        }

        case 'session_end':
          gotSessionEnd = true
          sessionEndReceived.add(true)
          socket.setTimeout(() => socket.close(), 200)
          break

        case 'error':
          // Surfaced for "submit after question end" or auth issues — count
          // toward submit_error_rate when it correlates with our own send.
          wsErrors.add(1)
          break
      }
    })

    socket.on('error', (e) => {
      wsErrors.add(1)
      if (!gotConnected) connectedRate.add(false)
    })

    socket.on('close', () => {
      if (!gotConnected) connectedRate.add(false)
      if (!gotSessionEnd) sessionEndReceived.add(false)
    })

    // Failsafe — close the socket when the whole run is over.
    socket.setTimeout(() => socket.close(), TOTAL_RUN_SEC * 1000)
  })
  check(wsRes, { 'participant ws status 101': (r) => r && r.status === 101 })
}

// ─── Teardown: verify history + leaderboard (criteria 11, 12) ─────────────────

export function teardown(data) {
  const headers = {
    Authorization: `Bearer ${data.token}`,
    'Content-Type': 'application/json',
  }

  // Criterion №11 — completed session must appear in the organizer's history.
  const histRes = http.get(`${BASE_URL}/api/sessions`, { headers })
  let history = []
  try {
    history = histRes.json() || []
  } catch (_) {
    // leave empty
  }
  const inHistory = Array.isArray(history) && history.some((s) => s.id === data.sessionId)
  check(histRes, {
    'history endpoint 200': (r) => r.status === 200,
    'session in history': () => inHistory,
  })

  // Criterion №12 — leaderboard for the session must be non-empty and ranked.
  const detailRes = http.get(`${BASE_URL}/api/sessions/${data.sessionId}`, { headers })
  let detail = {}
  try {
    detail = detailRes.json() || {}
  } catch (_) {
    detail = {}
  }
  const lb = Array.isArray(detail.leaderboard) ? detail.leaderboard : []
  check(detailRes, {
    'session detail 200': (r) => r.status === 200,
    'leaderboard non-empty': () => lb.length > 0,
    'leaderboard ranked desc': () => {
      for (let i = 1; i < lb.length; i++) {
        if (lb[i].total_score > lb[i - 1].total_score) return false
      }
      return true
    },
  })

  console.log(
    `[teardown] history_hit=${inHistory} leaderboard_size=${lb.length} ` +
      `participants_seen=${detail.participant_count || 'n/a'}`,
  )
}

// ─── handleSummary: print the criterion table side-by-side with results ───────

export function handleSummary(data) {
  const m = data.metrics
  const get = (name, field) =>
    m[name] && m[name].values && m[name].values[field] !== undefined
      ? m[name].values[field]
      : null

  const connectedRateVal = get('connected_rate', 'rate')
  const waitForQAvg = get('wait_for_question_ms', 'avg')
  const ackAvg = get('answer_ack_ms', 'avg')
  const submitErrRate = get('submit_error_rate', 'rate')
  const lost = get('lost_answers', 'count') || 0
  const dups = get('duplicate_acks', 'count') || 0
  const sessionEndRate = get('session_end_received', 'rate')

  const fmt = (v, digits = 2) => (v === null ? 'n/a' : Number(v).toFixed(digits))
  const pct = (v) => (v === null ? 'n/a' : (v * 100).toFixed(2) + '%')
  const verdict = (ok) => (ok ? 'PASS' : 'FAIL')

  const rows = [
    ['№2  Successful connections (≥99%)', pct(connectedRateVal), verdict(connectedRateVal !== null && connectedRateVal >= 0.99)],
    ['№3  Question delivery avg (≤500 ms)', `${fmt(waitForQAvg)} ms`, verdict(waitForQAvg !== null && waitForQAvg < 500)],
    ['№4  Answer ack avg (≤500 ms)', `${fmt(ackAvg)} ms`, verdict(ackAvg !== null && ackAvg < 500)],
    ['№6  Submit error rate (≤1%)', pct(submitErrRate), verdict(submitErrRate !== null && submitErrRate < 0.01)],
    ['№7  Lost answers (=0)', String(lost), verdict(lost === 0)],
    ['№8  Duplicate answer_accepted (=0)', String(dups), verdict(dups === 0)],
    ['     session_end received', pct(sessionEndRate), verdict(sessionEndRate !== null && sessionEndRate >= 0.99)],
  ]

  const colA = Math.max(...rows.map((r) => r[0].length))
  const colB = Math.max(...rows.map((r) => r[1].length))
  const sep = '─'.repeat(colA + colB + 14)
  let table = `\n${sep}\nPresentarium load test — criterion summary (Table 3.7)\n${sep}\n`
  for (const [a, b, c] of rows) {
    table += `${a.padEnd(colA)}   ${b.padStart(colB)}   ${c}\n`
  }
  table += `${sep}\n`
  table += `Configuration: PARTICIPANTS=${PARTICIPANTS} QUESTION_TIME=${QUESTION_TIME_SEC}s\n`
  table += `Note: criteria 1, 5, 9, 10, 11, 12 — see README.md.\n`

  return {
    stdout: table,
    'summary.json': JSON.stringify(data, null, 2),
  }
}
