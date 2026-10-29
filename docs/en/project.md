# Project scope and architecture

[Русский](../ru/project.md) | [Verification](verification.md)

atblse 0.2.0-rc.3 is an experimental local server and diagnostic panel for owners of stock VacuGlide 2 (`1.03`/`1.0.3`) and developers of compatible control clients. It runs without internet once its binaries, configuration and required script files are present.

| User journey             | Expected result                                                          | Acceptance boundary                                                                            |
| ------------------------ | ------------------------------------------------------------------------ | ---------------------------------------------------------------------------------------------- |
| Connect locally          | Recognized device, explicit pairing and independent local key            | Virtual TLS/WSS/auth tests pass; target network/device acceptance remains open                 |
| Use an external client   | Manual speed, both valves, scripts, 30 slots and confirmed protocol STOP | SDK 2.6.0 and Studio adapter contracts; installed client and physical behavior remain separate |
| Develop without hardware | Temporary loopback lab, synthetic replies and local transfers            | Standalone simulator and malformed-input tests                                                 |

The portable Go core owns TLS/WSS, HTTP/SSE, optional DNS, authentication and local script storage. The embedded browser panel provides bounded manual runs and STOP. The Windows wrapper owns its access point, selected-interface configuration and server lifecycle. Docker hosts the core independently and starts only by explicit command. Native Linux/macOS wrappers and Android hosting are not implemented.

The control contract contains 26 compatible operations. Local catalog import preserves native script identity and valve events. Cloud accounts, video hosting/catalog browsing, community, AI features, AI Ultra support and firmware updates are outside the current product. The server does not download binaries, dependencies or content at startup. An explicit URL import may access an allowed origin; use local files/origins for a fully offline workflow.

Persistent state is described in [data and recovery](data.md). The service supports one device, 30 slots and bounded stores/requests; [configuration](configuration.md) states exact limits. Docker's 96 MiB/0.5 CPU configuration is a candidate budget, not a measured capacity promise. No stable SLA, recovery time or data-loss guarantee is advertised.

Support covers this candidate and firmware profile. Report reproducible issues with version, sanitized configuration, expected/actual behavior and synthetic inputs. Do not attach keys, full private state, firmware images, session captures or vendor video files. Physical acceptance, installed lifecycle/restore, accessibility, capacity and release/license decisions remain open. See [verification](verification.md); a green software check does not close those gates.
