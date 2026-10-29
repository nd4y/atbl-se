"""Wire format observed on stock VacuGlide 1.03. No firmware modifications."""
import uuid

# 43/44 observed during the real 5%/STOP test on firmware 1.03.
MODES = {43: 'TARGET_SPEED_PLAYING', 44: 'TARGET_SPEED_PAUSED', 50: 'ONLINE_CONNECTED'}
COMMANDS = {'connected-ok', 'state', 'target-speed-stop', 'target-speed-set',
            'valve-stroke-plus-set', 'valve-stroke-minus-set'}


def request(name, args=()):
    if name not in COMMANDS:
        raise ValueError('Unsupported command')
    if name == 'target-speed-set':
        if len(args) != 1 or type(args[0]) is not int or not 0 <= args[0] <= 100:
            raise ValueError('Speed must be an integer from 0 to 100')
    elif name.startswith('valve-'):
        if len(args) != 1 or type(args[0]) is not int or args[0] not in (0, 1):
            raise ValueError('Valve must be 0 or 1')
    elif args:
        raise ValueError('Unexpected arguments')
    correlation = str(uuid.uuid4())
    return correlation, '/' + '/'.join([name, *map(str, args), correlation])


def state_message(text):
    parts = text.split('/')
    if len(parts) != 12 or parts[0] or parts[1] != 'state':
        raise ValueError('Unsupported device message')
    correlation = str(uuid.UUID(parts[-1]))
    values = list(map(int, parts[2:10]))
    mode, script, speed, plus, minus, current, offset, loop = values
    if not (0 <= speed <= 100 and plus in (0, 1) and minus in (0, 1) and loop in (0, 1)):
        raise ValueError('Invalid device state')
    return correlation, dict(operationalMode=MODES.get(mode, 'UNKNOWN_' + str(mode)),
                            rawOperationalMode=mode, localScript=script, targetSpeed=speed,
                            strokePlusValve=bool(plus), strokeMinusValve=bool(minus),
                            syncScriptCurrentTime=current, syncScriptOffsetTime=offset,
                            syncScriptLoop=bool(loop), syncScriptToken=parts[10])


def public_state(state):
    return {k: v for k, v in state.items() if k != 'syncScriptToken'}
