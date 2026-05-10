// Presentarium WebSocket stress / breakpoint test.
//
// Unlike room_load_test.js (fixed N participants, strict PASS/FAIL on Table 3.7),
// this scenario ramps participants from 0 → MAX over ~10 minutes to find the
// point where the system degrades or refuses connections. There are NO
// thresholds set — k6 will always exit 0 so we can inspect the curves.
//
// Run from the loadtest/ directory:
//
//   # HTML graphs in report.html (k6 v0.49+)
//   K6_WEB_DASHBOARD=true \
//   K6_WEB_DASHBOARD_EXPORT=report.html \
//   k6 run stress_test.js
//
//   # ad-hoc tweaking — the same env vars as room_load_test.js are honoured
//   k6 run -e MAX_PARTICIPANTS=2000 -e QUESTION_TIME=15 stress_test.js
//
// Required env vars (defaults shown):
//
//   BASE_URL          http://localhost:8080
//   MAX_PARTICIPANTS  2000      — peak concurrent participants
//   QUESTION_TIME     15        — seconds per question (shorter = more cycles)
//   STAGE_PROFILE     default   — see STAGE_PROFILES below
//
// Output:
//
//   report.html       — k6 web dashboard (when K6_WEB_DASHBOARD_EXPORT set)
//   stress_summary.json — full JSON metrics for further analysis
//
// What you get on the graphs:
//
//   • vus over time                              — load curve (ramp shape)
//   • wait_for_question_ms (avg / p95)           — question delivery latency
//   • answer_ack_ms       (avg / p95)            — answer round-trip
//   • connected_rate                             — fraction of attempted
//                                                  connections that succeeded
//   • submit_error_rate / lost_answers           — when answers start dropping
//   • ws_connecting / ws_session_duration        — k6 built-ins for WS health
//
// Read the inflection point on the latency curves against the vus axis:
// where p95 leaves its baseline and shoots up is the practical capacity.

import http from 'k6/http'
import ws from 'k6/ws'
import { check } from 'k6'
import { Trend, Counter, Rate } from 'k6/metrics'

// ─── Config ───────────────────────────────────────────────────────────────────

const BASE_URL = __ENV.BASE_URL || 'http://localhost:8080'
const WS_BASE_URL = BASE_URL.replace(/^http/, 'ws')
const MAX_PARTICIPANTS = parseInt(__ENV.MAX_PARTICIPANTS || '2000', 10)
const QUESTION_TIME_SEC = parseInt(__ENV.QUESTION_TIME || '15', 10)

// Ramp profiles. "default" climbs to MAX_PARTICIPANTS over ~10 min in 6 steps,
// then holds for 1 min so the dashboard captures the steady state at peak.
const STAGE_PROFILES = {
  default: [
    { duration: '60s', target: Math.round(MAX_PARTICIPANTS * 0.05) }, //   5%
    { duration: '60s', target: Math.round(MAX_PARTICIPANTS * 0.125) }, // 12.5%
    { duration: '60s', target: Math.round(MAX_PARTICIPANTS * 0.25) }, //  25%
    { duration: '120s', target: Math.round(MAX_PARTICIPANTS * 0.5) }, //  50%
    { duration: '120s', target: Math.round(MAX_PARTICIPANTS * 0.75) }, // 75%
    { duration: '120s', target: MAX_PARTICIPANTS }, //                  100%
    { duration: '60s', target: MAX_PARTICIPANTS }, //                   hold
    { duration: '30s', target: 0 }, //                                  drain
  ],
  // Short profile for smoke-testing the script itself.
  smoke: [
    { duration: '30s', target: 50 },
    { duration: '30s', target: 100 },
    { duration: '15s', target: 0 },
  ],
}

const profileName = __ENV.STAGE_PROFILE || 'default'
const stages = STAGE_PROFILES[profileName] || STAGE_PROFILES['default']
const TOTAL_RUN_SEC = stages.reduce((acc, s) => acc + parseInt(s.duration, 10), 0)
// Organizer needs to outlive the longest VU; +30s buffer for graceful close.
const ORGANIZER_DURATION_SEC = TOTAL_RUN_SEC + 30

// ─── Custom metrics ───────────────────────────────────────────────────────────

const connectedRate = new Rate('connected_rate')
const waitForQuestionMs = new Trend('wait_for_question_ms', true)
const answerAckMs = new Trend('answer_ack_ms', true)
const submitErrorRate = new Rate('submit_error_rate')
const lostAnswers = new Counter('lost_answers')
const duplicateAcks = new Counter('duplicate_acks')
const wsErrors = new Counter('ws_errors')

// ─── k6 scenarios ─────────────────────────────────────────────────────────────

export const options = {
  scenarios: {
    organizer: {
      executor: 'shared-iterations',
      vus: 1,
      iterations: 1,
      maxDuration: `${ORGANIZER_DURATION_SEC + 30}s`,
      exec: 'organizer',
    },
    participants: {
      executor: 'ramping-vus',
      startVUs: 0,
      stages: stages,
      // Each VU runs the function once and holds the WS open until the test ends.
      gracefulRampDown: '15s',
      gracefulStop: '30s',
      exec: 'participant',
      startTime: '5s', // give the organizer a moment to start the session
    },
  },
  // No thresholds — we want to *find* the breakpoint, not abort at it.
  // Inspect the dashboard for the point where p95 latency leaves baseline.
  noConnectionReuse: false,
  discardResponseBodies: false,
}

// ─── Setup: register organizer, create poll + 5 questions + room ──────────────

export function setup() {
  const suffix = Math.random().toString(36).slice(2, 10)
  const email = `stress-${suffix}@k6.test`

  const regRes = http.post(
    `${BASE_URL}/api/auth/register`,
    JSON.stringify({
      email,
      password: 'stress-test-pass-1234',
      name: 'Stress Test Organizer',
    }),
    { headers: { 'Content-Type': 'application/json' } },
  )
  if (regRes.status !== 201) {
    throw new Error(`register failed: ${regRes.status} ${regRes.body}`)
  }
  const token = regRes.json('access_token')

  const pollRes = http.post(
    `${BASE_URL}/api/polls`,
    JSON.stringify({
      title: `Stress test ${suffix}`,
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

  const questionPlans = [
    {
      type: 'single_choice',
      text: 'Stress Q1: 2+2?',
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
      text: 'Stress Q2: free text',
      time_limit_seconds: QUESTION_TIME_SEC,
      points: 0,
    },
    {
      type: 'word_cloud',
      text: 'Stress Q3: word cloud',
      time_limit_seconds: QUESTION_TIME_SEC,
      points: 0,
    },
    {
      type: 'single_choice',
      text: 'Stress Q4: capital of France?',
      time_limit_seconds: QUESTION_TIME_SEC,
      points: 100,
      options: [
        { text: 'London', is_correct: false },
        { text: 'Paris', is_correct: true },
        { text: 'Berlin', is_correct: false },
      ],
    },
    {
      type: 'open_text',
      text: 'Stress Q5: anything?',
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
    `[setup] stress poll=${pollId} room=${roomCode} session=${sessionId} ` +
      `max_vus=${MAX_PARTICIPANTS} total_run=${TOTAL_RUN_SEC}s profile=${profileName}`,
  )

  return { token, roomCode, sessionId, questionIds, pollId }
}

// ─── Organizer VU ─────────────────────────────────────────────────────────────
//
// Loops through the question list continuously for the entire test duration so
// participants joining at any ramp stage have something to answer.

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

  const endAt = Date.now() + ORGANIZER_DURATION_SEC * 1000

  ws.connect(url, {}, (socket) => {
    let qIndex = 0

    socket.on('open', () => {
      socket.setTimeout(showNext, 2000) // small warmup before the first question
    })

    function showNext() {
      if (Date.now() >= endAt) {
        // Drain phase reached — end the session and close.
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
      // Loop the question list forever — modulo over the array.
      const q = data.questionIds[qIndex % data.questionIds.length]
      qIndex++
      socket.send(
        JSON.stringify({
          type: 'show_question',
          data: { question_id: q.id },
        }),
      )
      socket.setTimeout(showNext, (QUESTION_TIME_SEC + 2) * 1000)
    }

    socket.on('error', () => wsErrors.add(1))

    socket.setTimeout(() => socket.close(), ORGANIZER_DURATION_SEC * 1000)
  })
}

// ─── Participant VU ───────────────────────────────────────────────────────────

export function participant(data) {
  const name = `vu-${__VU}`
  const url = `${WS_BASE_URL}/ws/room/${data.roomCode}?name=${encodeURIComponent(name)}`

  // Stay connected until the test ends — k6's ramping executor will signal
  // graceful shutdown by interrupting the iteration.
  const HOLD_MS = ORGANIZER_DURATION_SEC * 1000

  const submittedAt = {}
  const acked = new Set()
  let gotConnected = false

  ws.connect(url, {}, (socket) => {
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
          if (typeof msg.data.sent_at_ms === 'number' && msg.data.sent_at_ms > 0) {
            waitForQuestionMs.add(Date.now() - msg.data.sent_at_ms)
          }

          submittedAt[qid] = Date.now()
          let payload
          if (qtype === 'single_choice' || qtype === 'image_choice') {
            payload = { type: 'submit_answer', data: { question_id: qid, answer: 0 } }
          } else if (qtype === 'multiple_choice') {
            payload = { type: 'submit_answer', data: { question_id: qid, answer: [0] } }
          } else if (qtype === 'open_text' || qtype === 'word_cloud') {
            payload = {
              type: 'submit_text',
              data: { question_id: qid, text: `${name}-${qid.slice(0, 6)}` },
            }
          } else {
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
          const eqid = msg.data.question_id
          if (submittedAt[eqid] !== undefined && !acked.has(eqid)) {
            lostAnswers.add(1)
          }
          // Reset per-question state so the next loop iteration starts clean.
          delete submittedAt[eqid]
          acked.delete(eqid)
          break
        }

        case 'session_end':
          socket.setTimeout(() => socket.close(), 200)
          break

        case 'error':
          wsErrors.add(1)
          break
      }
    })

    socket.on('error', () => {
      wsErrors.add(1)
      if (!gotConnected) connectedRate.add(false)
    })

    socket.on('close', () => {
      if (!gotConnected) connectedRate.add(false)
    })

    socket.setTimeout(() => socket.close(), HOLD_MS)
  })
}

// ─── handleSummary: write JSON for archival; HTML comes from web-dashboard ────

export function handleSummary(data) {
  const m = data.metrics
  const get = (name, field) =>
    m[name] && m[name].values && m[name].values[field] !== undefined
      ? m[name].values[field]
      : null

  const peakVus = get('vus_max', 'value')
  const connectedRateVal = get('connected_rate', 'rate')
  const waitAvg = get('wait_for_question_ms', 'avg')
  const waitP95 = get('wait_for_question_ms', 'p(95)')
  const ackAvg = get('answer_ack_ms', 'avg')
  const ackP95 = get('answer_ack_ms', 'p(95)')
  const submitErr = get('submit_error_rate', 'rate')
  const lost = get('lost_answers', 'count') || 0
  const dups = get('duplicate_acks', 'count') || 0
  const wsErr = get('ws_errors', 'count') || 0

  const fmt = (v, d = 2) => (v === null ? 'n/a' : Number(v).toFixed(d))
  const pct = (v) => (v === null ? 'n/a' : (v * 100).toFixed(2) + '%')

  const rows = [
    ['Peak VUs reached', String(peakVus || 'n/a')],
    ['Successful connections', pct(connectedRateVal)],
    ['Question delivery avg', `${fmt(waitAvg)} ms`],
    ['Question delivery p95', `${fmt(waitP95)} ms`],
    ['Answer ack avg', `${fmt(ackAvg)} ms`],
    ['Answer ack p95', `${fmt(ackP95)} ms`],
    ['Submit error rate', pct(submitErr)],
    ['Lost answers (total)', String(lost)],
    ['Duplicate acks (total)', String(dups)],
    ['WS errors (total)', String(wsErr)],
  ]

  const colA = Math.max(...rows.map((r) => r[0].length))
  const colB = Math.max(...rows.map((r) => r[1].length))
  const sep = '─'.repeat(colA + colB + 6)

  let table = `\n${sep}\nPresentarium STRESS test summary\n${sep}\n`
  for (const [a, b] of rows) {
    table += `${a.padEnd(colA)}   ${b.padStart(colB)}\n`
  }
  table += `${sep}\n`
  table += `Profile: ${profileName}  Max VUs: ${MAX_PARTICIPANTS}  Run: ${TOTAL_RUN_SEC}s\n`
  table += `Look at report.html (web-dashboard) for the time-series graphs.\n`

  return {
    stdout: table,
    'stress_summary.json': JSON.stringify(data, null, 2),
  }
}
