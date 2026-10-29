import random
import unittest
from vacuglide_tick_model import TickPlayer, steps, schedule_resident, PAUSED_SYNC


class TickSemanticsTest(unittest.TestCase):
    def test_equal_times_are_not_atomic(self):
        p = TickPlayer(steps((0, 1, 1), (0, 2, 1), (0, 0, 60))).start()
        self.assertEqual([x['wall_ms'] for x in p.run()], [0, 10, 20])
        self.assertTrue(p.trace[1]['minus'] and p.trace[1]['plus'])

    def test_open_close_one_ms_is_expanded_to_one_tick(self):
        p = TickPlayer(steps((0, 1, 255), (1, 1, 0))).start()
        p.run()
        self.assertEqual([x['wall_ms'] for x in p.trace], [0, 10])
        self.assertTrue(p.trace[0]['minus'])
        self.assertFalse(p.minus)

    def test_dense_noops_delay_valve_close(self):
        rows = steps((0, 1, 1)) + steps(*[(i, 0, 0) for i in range(1, 100)]) + steps((100, 1, 0))
        p = TickPlayer(rows).start()
        p.run()
        self.assertEqual(p.trace[-1]['wall_ms'], 1000)
        self.assertEqual(p.trace[-1]['logical_lateness_ms'], 900)

    def test_unknown_opcode_consumes_capacity_without_state_change(self):
        p = TickPlayer(steps((0, 7, 255), (0, 1, 1)), speed=40).start()
        p.run()
        self.assertEqual(p.trace[0]['target_speed'], 40)
        self.assertEqual(p.trace[1]['wall_ms'], 10)

    def test_native_pause_does_not_close_valves(self):
        p = TickPlayer(steps((0, 1, 1), (1000, 1, 0))).start()
        p.advance(100)
        p.stop()
        p.advance(2000)
        self.assertEqual(p.mode, PAUSED_SYNC)
        self.assertTrue(p.minus)
        self.assertEqual(len(p.trace), 1)
        p.close_valves()
        self.assertFalse(p.minus)

    def test_natural_end_can_leave_valve_open(self):
        p = TickPlayer(steps((0, 0, 30), (100, 1, 1))).start()
        p.run()
        self.assertEqual(p.mode, PAUSED_SYNC)
        self.assertTrue(p.minus)
        self.assertEqual(p.speed, 30)  # register, not measured motion

    def test_seek_selects_next_event_and_does_not_restore_prefix(self):
        p = TickPlayer(steps((0, 0, 60), (100, 1, 1), (1000, 0, 20), (1500, 1, 0))).start(500)
        p.advance(490)
        self.assertEqual(p.trace, [])
        self.assertEqual(p.speed, 0)
        self.assertFalse(p.minus)
        p.advance(500)
        self.assertEqual(p.trace[0]['at_ms'], 1000)

    def test_restarting_closes_valves_even_with_no_matching_event(self):
        p = TickPlayer(steps((0, 1, 1), (1000, 1, 0))).start()
        p.advance(100)
        self.assertTrue(p.minus)
        p.start(200)
        self.assertFalse(p.minus)

    def test_signed_negative_effective_clock_waits(self):
        p = TickPlayer(steps((0, 0, 10), (100, 0, 0))).start(offset_ms=-25)
        p.run()
        self.assertEqual([x['wall_ms'] for x in p.trace], [30, 130])
        self.assertEqual([x['clock_before_ms'] for x in p.trace], [30, 130])

    def test_setup_time_and_tick_phase_are_explicit_assumptions(self):
        p = TickPlayer(steps((100, 0, 20), (1000, 0, 0))).start(90, setup_ms=15, phase_ms=7)
        p.advance(22)
        self.assertEqual(p.trace[0]['wall_ms'], 22)
        self.assertEqual(p.trace[0]['clock_before_ms'], 105)

    def test_sync_loop_is_async_reload_not_fictitious_instant_restart(self):
        p = TickPlayer(steps((0, 1, 1), (100, 0, 0))).start(loop=True)
        p.run()
        self.assertTrue(p.reload_requested)
        self.assertTrue(p.minus)
        self.assertEqual(len(p.trace), 2)

    def test_single_page_local_loop_retains_valve(self):
        p = TickPlayer(steps((0, 0, 20), (100, 1, 1))).start(local=True, offset_ms=1000)
        p.advance(110)
        self.assertEqual([x['wall_ms'] for x in p.trace], [0, 100, 110])
        self.assertTrue(p.minus)

    def test_seek_bounds_and_final_clock_increment(self):
        p = TickPlayer(steps((0, 0, 20), (100, 0, 0))).start(100)
        p.run()
        self.assertEqual(p.time_ms, 110)
        with self.assertRaises(ValueError):
            p.start(101)

    def test_512_page_boundary_resident_records_not_skipped(self):
        p = TickPlayer(steps(*[(i*10, 0, i % 100) for i in range(514)])).start()
        p.run()
        self.assertEqual(len(p.trace), 514)
        self.assertEqual(p.trace[512]['wall_ms'], 5120)

    def test_random_schedules_match_independent_capacity_solver(self):
        rng = random.Random(20261028)
        for case in range(160):
            at = 0
            rows = []
            for i in range(80):
                at += rng.choice([0, 1, 3, 9, 10, 11, 50])
                rows += steps((at, rng.randrange(5), rng.randrange(101)))
            start = rng.randrange(rows[-1]['at_ms']+1)
            offset, phase = rng.randrange(-100, 101), rng.randrange(10)
            p = TickPlayer(rows).start(start, offset_ms=offset, phase_ms=phase)
            got = [x['wall_ms'] for x in p.run()]
            self.assertEqual(got, schedule_resident(rows, start_ms=start, offset_ms=offset, phase_ms=phase), case)


if __name__ == '__main__':
    unittest.main()
