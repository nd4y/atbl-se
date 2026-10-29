"""Offline control-flow model of VacuGlide 1.03 worker 0x400d9e08.

Models commanded registers, not mechanics. Records are assumed memory-resident;
SPIFFS/preload latency, RTOS wall-clock jitter and asynchronous loop reload are
not inferred. The C/assembly sources identify the evidence in the report.
"""
from bisect import bisect_left

MASK = 0xffffffff
PLAYING_SYNC = 60
PAUSED_SYNC = 70
PLAYING_LOCAL = 41


class TickPlayer:
    def __init__(self, steps, *, speed=0, minus=False, plus=False):
        self.steps = [dict(x) for x in steps]
        if not self.steps or any(x['at_ms'] < 0 or x['at_ms'] > MASK
                                 for x in self.steps):
            raise ValueError('nonempty uint32 timeline required')
        if any(a['at_ms'] > b['at_ms'] for a, b in zip(self.steps, self.steps[1:])):
            raise ValueError('records must be ordered; equal times allowed')
        for x in self.steps:
            if any(type(x[k]) is not int for k in ('at_ms', 'opcode', 'value')):
                raise ValueError('integer fields required')
            if not 0 <= x['opcode'] <= 255 or not 0 <= x['value'] <= 255:
                raise ValueError('byte fields required')
        self.speed, self.minus, self.plus = speed, minus, plus
        self.time_ms = 0
        self.offset_ms = 0
        self.index = 0
        self.mode = PAUSED_SYNC
        self.loop = False
        self.next_wall_ms = 0
        self.reload_requested = False
        self.trace = []

    @property
    def encoded_end_ms(self):
        return self.steps[-1]['at_ms']

    def start(self, at_ms=0, *, offset_ms=0, phase_ms=0, setup_ms=0,
              local=False, loop=False):
        if not 0 <= phase_ms < 10 or setup_ms < 0:
            raise ValueError('phase must be 0..9 ms; setup nonnegative')
        self.index = 0 if local else bisect_left([x['at_ms'] for x in self.steps], at_ms)
        if self.index >= len(self.steps):
            # Native seek has no end guard. Refuse to invent out-of-bounds data.
            raise ValueError('native seek would run beyond the final record')
        self.mode = PLAYING_LOCAL if local else PLAYING_SYNC
        self.time_ms = 0 if local else (at_ms + setup_ms) & MASK
        self.offset_ms, self.loop = offset_ms, loop
        self.next_wall_ms = setup_ms + phase_ms
        self.reload_requested = False
        if not local:
            # 0x400d96e8 closes both GPIOs; targetSpeed is not reset here.
            self.minus = self.plus = False
        return self

    def stop(self):
        # 0x400d97ec only changes mode. Register values remain unchanged.
        self.mode = PAUSED_SYNC

    def close_valves(self):
        self.minus = self.plus = False

    def tick(self):
        if self.mode not in (PLAYING_SYNC, PLAYING_LOCAL) or self.reload_requested:
            return None
        clock_before = self.time_ms
        offset = self.offset_ms if self.mode == PLAYING_SYNC else 0
        effective = (clock_before + offset) & MASK
        # Worker advances clock BEFORE the signed guard/due-record comparison.
        self.time_ms = (clock_before + 10) & MASK
        wall = self.next_wall_ms
        self.next_wall_ms += 10
        if effective >= 0x80000000:
            return None
        row = self.steps[self.index]
        if row['at_ms'] > effective:
            return None
        opcode, value = row['opcode'], row['value']
        if opcode == 0:
            self.speed = value
        elif opcode == 1:
            self.minus = bool(value)
        elif opcode == 2:
            self.plus = bool(value)
        event = dict(row, index=self.index, wall_ms=wall,
                     clock_before_ms=clock_before, effective_ms=effective,
                     logical_lateness_ms=effective-row['at_ms'],
                     target_speed=self.speed, minus=self.minus, plus=self.plus)
        self.trace.append(event)
        if self.index + 1 < len(self.steps):
            self.index += 1
        elif self.mode == PLAYING_LOCAL:
            if len(self.steps) > 512:
                # Final-buffer reset is not sufficient evidence of whole-file
                # local looping across pages. Leave its loader path unmodeled.
                self.reload_requested = True
            else:
                self.index = 0
                self.time_ms = 0
        elif self.loop:
            # Native code enqueues task (2,0). Its real loading gap is unknown.
            self.reload_requested = True
        else:
            self.mode = PAUSED_SYNC
        return event

    def advance(self, wall_ms, *, max_ticks=2_000_000):
        calls = 0
        while self.next_wall_ms <= wall_ms and self.mode in (PLAYING_SYNC, PLAYING_LOCAL) and not self.reload_requested:
            self.tick()
            calls += 1
            if calls > max_ticks:
                raise RuntimeError('simulation tick bound exceeded')
        return self.trace

    def run(self, *, max_ticks=2_000_000):
        calls = 0
        while self.mode in (PLAYING_SYNC, PLAYING_LOCAL) and not self.reload_requested:
            self.tick()
            calls += 1
            if calls > max_ticks:
                raise RuntimeError('simulation tick bound exceeded')
        return self.trace


def steps(*rows):
    return [dict(at_ms=t, opcode=op, value=v) for t, op, v in rows]


def schedule_resident(records, *, start_ms=0, offset_ms=0, phase_ms=0):
    """Independent algebraic schedule, for ordinary non-wrapping sync clocks.

    One-record service capacity: event i runs no earlier than i-1 plus one tick.
    This solver cross-checks the procedural model, including negative offsets.
    """
    index = bisect_left([r['at_ms'] for r in records], start_ms)
    previous_tick = -1
    result = []
    for r in records[index:]:
        effective_origin = start_ms + offset_ms
        # Signed guard delays every event while effective time is negative.
        due_tick = max(0, (r['at_ms']-effective_origin+9)//10)
        tick = max(previous_tick+1, due_tick)
        result.append(phase_ms+10*tick)
        previous_tick = tick
    return result
