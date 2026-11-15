# Operational Universe — what the engine can know, ask and do

## 0. Framing

The engine takes its vocabulary from open standards: STIX for entities and
reasoning objects (`01`), OCSF for telemetry (`03 §4`), MITRE ATT&CK for
technique labels (`01 §reckon-hypothesis`), MITRE D3FEND for response actions
(`04 §2`). Each was adopted where a feature needed it. The read side (`03`) and
the write side (`04`, `08`) are specified apart, which is the right lens for
authorization and the wrong one for completeness: neither says what the whole
is.

This spec is the lens above them. The **operational universe** is everything
the engine can, at least in principle, **know** (entity and object types),
**ask** (read verbs), **receive** (telemetry classes) and **do** (action
types). It is drawn from the standards, item by item, and every item carries a
disposition. The agent investigates, contains, eradicates and recovers inside
this universe; an adapter makes a part of it real for one tenant.

The engine runs no detection and no analysis of its own. Every item in the
universe is something a tenant's own tool knows or can do, reached through an
adapter. Where the tenant has the tool, the capability is available to the
agent. Where it does not, the verb answers `UNAVAILABLE_TENANT` (`03 §6.1`),
which is a different answer from having looked and found nothing.

Two rules follow.

- **Nothing is silently dropped.** An item of an adopted standard that the
  engine does not use says so, with the reason. `04 §2.2` already does this for
  D3FEND's three response tactics; this spec extends the same accounting to the
  rest of D3FEND and to STIX, OCSF and ATT&CK.
- **The catalogues are checked against it.** The verb catalogue (`03 §2`), the
  action catalogue (`04 §2.1`, `action.DefaultActionCatalog()`), the entity
  types (`01`) and the normalizers (`03 §4`) stay authoritative for what they
  define. Where one disagrees with this spec, the difference is listed in §8
  until one of them changes.

This spec adds no verb, action or type by itself. A row marked *proposed* is a
candidate; it becomes real through its own issue and decision record.

## 1. Dispositions and status

Every item carries exactly one **disposition**. Action, Reversal, Handoff and
Recommendation are `04 §2.2`'s, unchanged (its *Architecture* is named
*Recommendation* here, for what the product does with it).

| Disposition | Meaning |
|---|---|
| **Entity** | a type the graph holds: a STIX object or one of the engine's extensions (`01`) |
| **Telemetry** | an OCSF class a normalizer turns into observations (`03 §4`) |
| **Read** | a question the agent asks through a capability verb (`03 §2`). The source may answer from stored telemetry, from a live query, or from an analysis it has already made |
| **Action** | a state change the engine dispatches through `request_action` (`04 §2.2`) |
| **Reversal** | the undo half of an Action (`04 §2.2`) |
| **Handoff** | a real response step that is not agent-dispatchable; recorded and carried by `ticket.create` (`04 §2.2`) |
| **Recommendation** | a preventive or posture control with no incident-time dispatch; surfaces in post-conclusion outputs (`07`) |
| **Knowledge** | reference material the agent cites and reasons over, not an observation of the tenant |
| **Export** | an object the engine writes when it shares or archives, with no role inside an investigation |
| **Deferred** | in the universe, not yet given a shape; the note says what it waits on |
| **Out of scope** | outside the universe, with the reason |

**Status** applies to in-scope rows: *built* (in code today), *specified* (in a
spec, not in code), *proposed* (first named here).

A **family** row in D3FEND whose children are each dispositioned inherits
"per child". A child with no row of its own takes its parent's disposition,
written "as D3-…".

## 2. Versions

| Standard | Version this pass was checked against | How it was read |
|---|---|---|
| MITRE D3FEND | 1.6.0 | the ontology's technique matrix, every technique |
| OCSF | 1.9.0, the normalizers' ceiling (`03 §4.16`) | the schema checked in under `third_party/ocsf-schema/`, every class |
| STIX | the version `01` states (*STIX Conformance and Change*), which is the only place that states it | the object schemas checked in under `third_party/stix2-json-schemas/`, every object |
| MITRE ATT&CK | Enterprise matrix, tactic level | technique-level rows are not enumerated in this pass (§6) |

## 3. MITRE D3FEND

D3FEND has seven tactics and 272 techniques at 1.6.0. Isolate (57), Evict (19)
and Restore (12) are dispositioned in `04 §2.2`, which stays authoritative; the
counts there match 1.6.0. The four tactics below are new in this pass.

The principle for **Detect**: the engine does not run detections; a tenant's
tools do. So every Detect technique in scope is a *Read*. Where an
investigator asks the technique's question directly, the verb is the one that
returns its evidence. Where the analysis only exists inside a sensor, the
sensor raises a detection and the verb is `search_alerts`. For **Model**, the
engine reads what an inventory or scanner already knows. For **Harden**,
almost everything is posture; the few incident-time steps are Actions or
Handoffs. **Deceive** is out of scope: the engine models no decoys. An alert
that a tenant's decoy product raises still arrives through the tenant's alert
source like any other alert.

### Model (27 techniques)

14 Read, 8 Deferred, 5 Out of scope.

| D3FEND technique | Disposition | Status | Mapping |
|---|---|---|---|
| D3-SYSM System Mapping | Deferred | — | family: service and data-flow maps are not modelled |
| · D3-SYSVA System Vulnerability Assessment | Read | proposed | proposed `get_vulnerabilities` |
| · D3-SVCDM Service Dependency Mapping | Deferred | — | as D3-SYSM |
| · D3-DEM Data Exchange Mapping | Deferred | — | as D3-SYSM |
| · D3-SYSDM System Dependency Mapping | Deferred | — | as D3-SYSM |
| D3-OAM Operational Activity Mapping | Read | specified | family: per child |
| · D3-ODM Operational Dependency Mapping | Deferred | — | business-process dependencies are not modelled |
| · D3-OM Organization Mapping | Read | specified | `get_user_context` (manager, groups); host owner on `get_host_context` |
| · D3-AM Access Modeling | Read | built | `get_privilege_changes` (built), `get_user_context` (specified) |
| · D3-ORA Operational Risk Assessment | Out of scope | — | a governance process; its output reaches the engine as asset criticality on a target (04 §8.2) |
| D3-NM Network Mapping | Read | built | family: per child |
| · D3-NTPM Network Traffic Policy Mapping | Deferred | — | firewall and routing policy is not read |
| · D3-PLM Physical Link Mapping | Out of scope | — | physical layer |
| · · D3-DPLM Direct Physical Link Mapping | Out of scope | — | as D3-PLM |
| · · D3-APLM Active Physical Link Mapping | Out of scope | — | as D3-PLM |
| · D3-NVA Network Vulnerability Assessment | Read | proposed | proposed `get_vulnerabilities` (network-scanner findings) |
| · D3-LLM Logical Link Mapping | Read | built | observed links only: `get_network_connections` |
| · · D3-PLLM Passive Logical Link Mapping | Read | built | `get_network_connections` |
| · · D3-ALLM Active Logical Link Mapping | Out of scope | — | active probing is a scanner function, not an investigation read |
| D3-AI Asset Inventory | Read | built | family: inventory reads, per child |
| · D3-HCI Hardware Component Inventory | Read | built | `get_host_context` (device inventory, 03 §4.17), as far as the source reports hardware |
| · D3-CI Configuration Inventory | Read | proposed | cloud resources: `get_cloud_resource_context` (built). Hosts: proposed `get_configuration_state` |
| · D3-SWI Software Inventory | Read | proposed | proposed `get_software_inventory` |
| · D3-AVE Asset Vulnerability Enumeration | Read | proposed | proposed `get_vulnerabilities` |
| · · D3-CIA Container Image Analysis | Deferred | — | container images are not an entity type yet |
| · D3-DI Data Inventory | Deferred | — | no data-classification source is modelled |
| · D3-NNI Network Node Inventory | Read | built | `get_host_context` |

### Harden (56 techniques)

26 Out of scope, 24 Recommendation, 4 Handoff, 2 Action.

| D3FEND technique | Disposition | Status | Mapping |
|---|---|---|---|
| D3-SCH Source Code Hardening | Out of scope | — | software-development practice; no incident-time surface |
| · D3-VTV Variable Type Validation | Out of scope | — | as D3-SCH |
| · D3-CS Credential Scrubbing | Recommendation | — | after a leaked-credential incident (07) |
| · D3-VI Variable Initialization | Out of scope | — | as D3-SCH |
| · D3-RN Reference Nullification | Out of scope | — | as D3-SCH |
| · D3-DLV Domain Logic Validation | Out of scope | — | as D3-SCH |
| · · D3-OLV Operational Logic Validation | Out of scope | — | as D3-SCH |
| · D3-PV Pointer Validation | Out of scope | — | as D3-SCH |
| · · D3-MBSV Memory Block Start Validation | Out of scope | — | as D3-SCH |
| · · D3-NPC Null Pointer Checking | Out of scope | — | as D3-SCH |
| · D3-TL Trusted Library | Out of scope | — | as D3-SCH |
| · D3-IRV Integer Range Validation | Out of scope | — | as D3-SCH |
| D3-PH Platform Hardening | Recommendation | — | posture; surfaces in post-conclusion recommendations (07) |
| · D3-HBWP Hardware-based Write Protection | Out of scope | — | hardware |
| · D3-PEH Physical Enclosure Hardening | Out of scope | — | physical |
| · D3-DLIC Driver Load Integrity Checking | Recommendation | — | as D3-PH |
| · D3-SCP System Configuration Permissions | Recommendation | — | as D3-PH |
| · D3-FE File Encryption | Recommendation | — | as D3-PH |
| · D3-SU Software Update | Handoff | proposed | patching is change-managed: `ticket.create`. The read beside it is proposed `get_vulnerabilities` (patch state) |
| · D3-RH Radiation Hardening | Out of scope | — | physical |
| · · Particle Radiation Hardening | Out of scope | — | as D3-RH |
| · · D3-EMH Electromagnetic Radiation Hardening | Out of scope | — | as D3-RH |
| · · · D3-RFS RF Shielding | Out of scope | — | as D3-RH |
| · D3-TBI TPM Boot Integrity | Recommendation | — | as D3-PH |
| · D3-DENCR Disk Encryption | Recommendation | — | as D3-PH |
| · D3-BA Bootloader Authentication | Recommendation | — | as D3-PH |
| D3-AA Agent Authentication | Recommendation | — | authentication posture |
| · D3-CBAN Certificate-based Authentication | Recommendation | — | as D3-AA |
| · D3-BAN Biometric Authentication | Recommendation | — | as D3-AA |
| · D3-TBA Token-based Authentication | Recommendation | — | as D3-AA |
| · D3-MFA Multi-factor Authentication | Handoff | — | enforcing MFA on an account after a takeover is an identity-team change: `ticket.create` |
| · D3-PWA Password Authentication | Recommendation | — | as D3-AA |
| D3-CH Credential Hardening | Recommendation | — | credential posture |
| · D3-SPP Strong Password Policy | Recommendation | — | as D3-CH |
| · · D3-CDP Change Default Password | Recommendation | — | as D3-CH |
| · D3-CRO Credential Rotation | Action | specified | incident-time rotation is `credential.reset` (04 §2.2, D3-CR + D3-RIC); scheduled rotation is a Recommendation |
| · · D3-CERO Certificate Rotation | Handoff | — | certificate reissue is a PKI change: `ticket.create` |
| · · D3-PR Password Rotation | Action | specified | as D3-CRO |
| · · · D3-OTP One-time Password | Recommendation | — | credential posture |
| · D3-TB Token Binding | Recommendation | — | as D3-CH |
| · D3-CP Certificate Pinning | Recommendation | — | as D3-CH |
| D3-AH Application Hardening | Out of scope | — | build-time and runtime exploit mitigations |
| · D3-CFI Control Flow Integrity | Out of scope | — | as D3-AH |
| · D3-SFCV Stack Frame Canary Validation | Out of scope | — | as D3-AH |
| · D3-DCE Dead Code Elimination | Out of scope | — | as D3-AH |
| · D3-ACH Application Configuration Hardening | Recommendation | — | application configuration posture |
| · · D3-DRA Disable Remote Access | Handoff | — | disabling a remote-access service on a host: `ticket.create` |
| · D3-PSEP Process Segment Execution Prevention | Out of scope | — | as D3-AH |
| · D3-EHPV Exception Handler Pointer Validation | Out of scope | — | as D3-AH |
| · D3-SAOR Segment Address Offset Randomization | Out of scope | — | as D3-AH |
| · D3-PAN Pointer Authentication | Out of scope | — | as D3-AH |
| D3-MH Message Hardening | Recommendation | — | mail and message posture |
| · D3-MENCR Message Encryption | Recommendation | — | as D3-MH |
| · D3-TAAN Transfer Agent Authentication | Recommendation | — | as D3-MH |
| · D3-MAN Message Authentication | Recommendation | — | as D3-MH |
| · · D3-BMA Bus Message Authentication | Out of scope | — | bus-level |

### Detect (90 techniques)

69 Read, 13 Out of scope, 8 Deferred.

| D3FEND technique | Disposition | Status | Mapping |
|---|---|---|---|
| D3-UBA User Behavior Analysis | Read | built | family: per child |
| · D3-SDA Session Duration Analysis | Read | built | `enumerate_logons` |
| · D3-ANET Authentication Event Thresholding | Read | specified | `get_failed_auth_burst` |
| · D3-CCSA Credential Compromise Scope Analysis | Read | built | `enumerate_logons` by account, then `pivot_on_indicator` |
| · D3-AZET Authorization Event Thresholding | Read | built | cloud: `get_cloud_api_activity` with outcome FAILURE. Hosts: not read |
| · D3-UGLPA User Geolocation Logon Pattern Analysis | Read | built | `enumerate_logons` (source geolocation carried on the observation) |
| · D3-DAM Domain Account Monitoring | Read | built | `get_privilege_changes` |
| · D3-JFAPA Job Function Access Pattern Analysis | Read | built | `search_alerts`: a behaviour-analytics source raises it as a detection |
| · D3-RAPA Resource Access Pattern Analysis | Read | built | `get_cloud_api_activity`, `get_file_activity` |
| · D3-WSAA Web Session Activity Analysis | Read | proposed | proposed `get_web_activity` |
| · D3-UDTA User Data Transfer Analysis | Read | proposed | proposed `get_web_activity`; `get_network_connections` carries byte counts where the source reports them |
| · D3-LAM Local Account Monitoring | Read | built | `get_privilege_changes` |
| D3-FA File Analysis | Read | proposed | family: per child |
| · D3-FCOA File Content Analysis | Read | proposed | proposed `get_file_analysis` |
| · · D3-FCR File Content Rules | Read | proposed | proposed `get_file_analysis` (a rule scan of the file); a match the source raised itself is read with `search_alerts` |
| · D3-DA Dynamic Analysis | Read | proposed | proposed `get_file_analysis` (detonation verdict) |
| · D3-EFA Emulated File Analysis | Read | proposed | proposed `get_file_analysis` |
| · D3-FH File Hashing | Read | built | `get_file_activity` (hashes carried on the `file` entity) |
| D3-ID Identifier Analysis | Read | built | family: per child |
| · D3-UA URL Analysis | Read | built | `get_indicator_context` |
| · D3-IRA Identifier Reputation Analysis | Read | built | `get_indicator_context` |
| · · D3-FHRA File Hash Reputation Analysis | Read | built | as D3-IRA |
| · · D3-IPRA IP Reputation Analysis | Read | built | as D3-IRA |
| · · D3-DNRA Domain Name Reputation Analysis | Read | built | as D3-IRA |
| · · D3-URA URL Reputation Analysis | Read | built | as D3-IRA |
| · D3-HD Homoglyph Detection | Read | built | `search_alerts`: the mail or DNS source raises it as a detection |
| · D3-IAA Identifier Activity Analysis | Read | built | `pivot_on_indicator` |
| D3-PA Process Analysis | Read | built | family: per child |
| · D3-PSMD Process Self-Modification Detection | Read | built | `search_alerts`: the sensor raises it as a detection |
| · D3-DQSA Database Query String Analysis | Deferred | — | database telemetry is not read |
| · D3-SEA Script Execution Analysis | Read | built | `get_process_executions` (command lines, decoded where encoded) |
| · D3-SSC Shadow Stack Comparisons | Read | built | `search_alerts`: the sensor raises it as a detection |
| · D3-FAPA File Access Pattern Analysis | Read | built | `get_file_activity` |
| · D3-IBCA Indirect Branch Call Analysis | Read | built | `search_alerts`: the sensor raises it as a detection |
| · D3-SCA System Call Analysis | Read | built | `search_alerts`: the sensor raises it as a detection |
| · · D3-FCA File Creation Analysis | Read | built | `get_file_activity` |
| · D3-PCSV Process Code Segment Verification | Read | built | `search_alerts`: the sensor raises it as a detection |
| · D3-PSA Process Spawn Analysis | Read | built | `get_process_executions` |
| · · D3-PLA Process Lineage Analysis | Read | built | `get_process_ancestry` (built), `get_process_descendants` (specified) |
| D3-MA Message Analysis | Read | specified | `get_email_messages` |
| · D3-SMRA Sender MTA Reputation Analysis | Read | specified | as D3-MA |
| · D3-SRA Sender Reputation Analysis | Read | specified | as D3-MA |
| D3-PM Platform Monitoring | Read | built | family: per child |
| · D3-FEMC Firmware Embedded Monitoring Code | Out of scope | — | device firmware |
| · D3-OMM Operating Mode Monitoring | Out of scope | — | industrial control systems |
| · D3-OPM Operational Process Monitoring | Out of scope | — | industrial control systems |
| · D3-APM Application Performance Monitoring | Deferred | — | application performance and error telemetry is not read |
| · · D3-AEM Application Exception Monitoring | Deferred | — | as D3-APM |
| · D3-FIM File Integrity Monitoring | Read | built | `get_file_activity` |
| · D3-PUM Platform Uptime Monitoring | Deferred | — | availability monitoring is not read; a host's last contact is on `get_host_context` |
| · D3-FV Firmware Verification | Out of scope | — | device firmware |
| · · D3-PFV Peripheral Firmware Verification | Out of scope | — | as D3-FV |
| · · D3-SFV System Firmware Verification | Out of scope | — | as D3-FV |
| · D3-FBA Firmware Behavior Analysis | Out of scope | — | device firmware |
| · D3-OSM Operating System Monitoring | Read | built | family: per child |
| · · D3-SDM System Daemon Monitoring | Read | built | `get_persistence_artifacts` (services) |
| · · D3-USICA User Session Init Config Analysis | Read | built | `get_persistence_artifacts` |
| · · D3-IDA Input Device Analysis | Read | built | `search_alerts`: the sensor raises it as a detection |
| · · D3-SJA Scheduled Job Analysis | Read | built | `get_persistence_artifacts` (scheduled jobs) |
| · · D3-EHB Endpoint Health Beacon | Read | built | `get_host_context` (last seen, sensors) |
| · · D3-SFA System File Analysis | Read | built | `get_file_activity` |
| · · · D3-SBV Service Binary Verification | Read | built | as D3-SFA |
| · · D3-MBT Memory Boundary Tracking | Read | built | `search_alerts`: the sensor raises it as a detection |
| · · D3-SICA System Init Config Analysis | Read | built | `get_persistence_artifacts` |
| D3-PHAM Physical Access Monitoring | Out of scope | — | physical security |
| · D3-MSM Motion Sensor Monitoring | Out of scope | — | as D3-PHAM |
| · D3-PSM Proximity Sensor Monitoring | Out of scope | — | as D3-PHAM |
| · D3-VS Video Surveillance | Out of scope | — | as D3-PHAM |
| · D3-ELM Electronic Lock Monitoring | Out of scope | — | as D3-PHAM |
| D3-NTA Network Traffic Analysis | Read | built | family: per child |
| · D3-NTCD Network Traffic Community Deviation | Read | built | `search_alerts`: the network sensor raises it as a detection |
| · D3-ISVA Inbound Session Volume Analysis | Read | built | `get_network_connections` (sessions by destination and time) |
| · D3-FC File Carving | Deferred | — | files carved from traffic are not read |
| · D3-ANAA Administrative Network Activity Analysis | Read | specified | `get_lateral_movement_signals` |
| · D3-NTSA Network Traffic Signature Analysis | Read | built | `search_alerts`: the network sensor raises it as a detection |
| · D3-BSE Byte Sequence Emulation | Read | built | `search_alerts`: the network sensor raises it as a detection |
| · D3-CAA Connection Attempt Analysis | Read | built | `get_network_connections` |
| · D3-DNSTA DNS Traffic Analysis | Read | built | `resolve_dns` |
| · D3-RTA RPC Traffic Analysis | Read | specified | `get_lateral_movement_signals` |
| · D3-RPA Relay Pattern Analysis | Read | built | `search_alerts`: the network sensor raises it as a detection |
| · D3-CA Certificate Analysis | Deferred | — | certificates are not an entity type yet (STIX `x509-certificate`) |
| · · D3-PCA Passive Certificate Analysis | Deferred | — | as D3-CA |
| · · D3-ACA Active Certificate Analysis | Deferred | — | as D3-CA |
| · D3-APCA Application Protocol Command Analysis | Read | built | `search_alerts`: the network sensor raises it as a detection |
| · · D3-RFUM Remote Firmware Update Monitoring | Out of scope | — | device firmware |
| · D3-PHDURA Per Host Download-Upload Ratio Analysis | Read | built | `get_network_connections`, where the source reports byte counts |
| · D3-CSPP Client-server Payload Profiling | Read | built | `search_alerts`: the network sensor raises it as a detection |
| · D3-PMAD Protocol Metadata Anomaly Detection | Read | built | `search_alerts`: the network sensor raises it as a detection |
| · D3-RTSD Remote Terminal Session Detection | Read | built | `enumerate_logons` (remote logon types); `get_lateral_movement_signals` (specified) |
| · D3-IPCTA IPC Traffic Analysis | Read | built | `search_alerts`: the network sensor raises it as a detection |

### Deceive (11 techniques)

11 Out of scope.

| D3FEND technique | Disposition | Status | Mapping |
|---|---|---|---|
| D3-DO Decoy Object | Out of scope | — | deception is not modelled: no verb, entity or action for decoys |
| · D3-DPR Decoy Public Release | Out of scope | — | as D3-DO |
| · D3-DNR Decoy Network Resource | Out of scope | — | as D3-DO |
| · D3-DF Decoy File | Out of scope | — | as D3-DO |
| · D3-DST Decoy Session Token | Out of scope | — | as D3-DO |
| · D3-DUC Decoy User Credential | Out of scope | — | as D3-DO |
| · D3-DP Decoy Persona | Out of scope | — | as D3-DO |
| D3-DE Decoy Environment | Out of scope | — | as D3-DO |
| · D3-SHN Standalone Honeynet | Out of scope | — | as D3-DE |
| · D3-IHN Integrated Honeynet | Out of scope | — | as D3-DE |
| · D3-CHN Connected Honeynet | Out of scope | — | as D3-DE |

### Isolate, Evict, Restore (88 techniques)

See `04 §2.2`: 11 Action-covered and 46 Recommendation in Isolate; 10 Action
and 6 Handoff in Evict; 5 Reversal, 1 Action and 4 Handoff in Restore. Of the
action types those rows name, 9 are built and 8 are specified (`04 §2.1`):
`session.revoke`, `credential.reset`, `process.kill`, `process.suspend`,
`file.quarantine`, `file.restore`, `persistence.remove`, `host.reimage`.

### What the D3FEND pass shows

- **Detect is covered by reads.** 69 of the 90 rows are reads, 56 of them on
  a verb that is built. 17 of those rest on `search_alerts`: the engine reads
  the sensor's detection, and an empty answer does not show that the sensor
  runs that analysis (§8). The verbs carry no D3FEND id, so nothing can report
  which defensive techniques an investigation used (§8).
- **Model is where vulnerability and inventory live**, and it is the thinnest:
  three of its techniques wait on one proposed verb, `get_vulnerabilities`.
- **Eradication is specified and unbuilt.** Every Evict action beyond account
  locking and email removal is a `04 §2.1` roadmap row.

## 4. STIX

STIX splits into domain objects (the threat and its analysis), relationship
objects, cyber-observables and meta objects. The engine adopted the
observables and built its reasoning model as extensions (`01`). The version
and the conformance rules are in `01` (*STIX Conformance and Change*), and
every deviation from the standard is listed in `03 §7`; this section repeats
neither. The threat-side domain objects were never dispositioned.

### Domain objects (19)

| STIX object | Disposition | Status | Note |
|---|---|---|---|
| `attack-pattern` | Knowledge | proposed | an ATT&CK technique as an object a hypothesis references, in place of a free-text label (§6) |
| `campaign` | Deferred | — | the engine's analog is cross-investigation linkage (`07 §5`); the mapping is undecided |
| `course-of-action` | Export | proposed | an action type or SOP step, when a bundle is shared |
| `grouping` | Entity | built | the investigation's substrate (`01 §Grouping`) |
| `identity` | Entity | built | the vendor a sighting names, and the author of the extension definitions (`01`, `03 §4.12`). A cloud account is a `reckon-cloud-account`, not an identity |
| `incident` | Export | proposed | STIX leaves it a stub; the investigation is the engine's incident, mapped on export |
| `indicator` | Entity | built | what a source's detection becomes (`03 §4.12`); indicator extraction after a conclusion is specified (`07 §3`) |
| `infrastructure` | Deferred | — | attacker infrastructure is held as its observables (addresses, domains) today |
| `intrusion-set` | Knowledge | proposed | returned by proposed `get_threat_context` |
| `location` | Deferred | — | geolocation rides as attributes on a logon observation |
| `malware` | Knowledge | proposed | returned by proposed `get_threat_context` and `get_file_analysis` |
| `malware-analysis` | Entity | proposed | the result of proposed `get_file_analysis` |
| `note` | Entity | built | a person's attestation of a test they carried out (`01 §reckon-prediction`) |
| `observed-data` | Entity | built | `01 §ObservedData` |
| `opinion` | Entity | specified | adopted in `01`; nothing writes one (§8) |
| `report` | Export | specified | the export bundle and generated documents (`07 §2`, `§6`) |
| `threat-actor` | Knowledge | proposed | returned by proposed `get_threat_context` |
| `tool` | Knowledge | proposed | returned by proposed `get_threat_context` |
| `vulnerability` | Entity | proposed | a known flaw on a host or product, returned by proposed `get_vulnerabilities` |

### Relationship objects (2)

| STIX object | Disposition | Status | Note |
|---|---|---|---|
| `relationship` | Entity | built | `01 §Relationship` |
| `sighting` | Entity | built | `01 §Sighting` |

### Cyber-observables (18)

| STIX object | Disposition | Status | Note |
|---|---|---|---|
| `artifact` | Entity | built | stands for a stored record a normalizer extracts nothing from (`03 §4.13`). The engine still records references to evidence files, never the files |
| `autonomous-system` | Deferred | — | an attribute of an address in indicator context today |
| `directory` | Entity | built | `01` |
| `domain-name` | Entity | built | `01` |
| `email-addr` | Entity | built | `01` |
| `email-message` | Entity | built | `01` |
| `file` | Entity | built | `01` |
| `ipv4-addr` | Entity | built | `01` |
| `ipv6-addr` | Entity | built | `01` |
| `mac-addr` | Deferred | — | a host-identity input, not an entity today |
| `mutex` | Deferred | — | a malware-analysis output; waits on `get_file_analysis` |
| `network-traffic` | Entity | built | `01` |
| `process` | Entity | built | `01` |
| `software` | Entity | proposed | an installed product, for proposed `get_software_inventory` and `get_vulnerabilities` |
| `url` | Entity | built | `01` |
| `user-account` | Entity | built | `01`; a cloud principal is one too (`03 §4.15`) |
| `windows-registry-key` | Entity | built | `01`, `03 §4.6` |
| `x509-certificate` | Deferred | — | waits on certificate reads (D3-CA) |

### Meta objects (3)

| STIX object | Disposition | Status | Note |
|---|---|---|---|
| `marking-definition` | Export | specified | sharing sensitivity (`07 §8.2`) |
| `extension-definition` | Entity | built | declares each of the engine's own types and property sets (`01`) |
| `language-content` | Out of scope | — | no translated content |

### The engine's own types

Five custom observables (`reckon-host`, `reckon-scheduled-task`,
`reckon-group`, `reckon-cloud-account`, `reckon-cloud-resource`) and the
reasoning objects (`reckon-null-observation`, `reckon-hypothesis`,
`reckon-prediction`, `reckon-action`, `reckon-interpretation`) are defined in
`01`, each declared by an extension definition, and are all built. They are
the part of the universe no standard supplies.

## 5. OCSF

OCSF 1.9.0 has 87 classes. 22 are built (21 with a normalizer, and the base
class as the wrapper for an untyped row), 30 are proposed, 28 are deferred and
7 are out of scope. A class is *proposed* when a verb, built or proposed,
needs it to answer its question on a real source.

### Base (1 class)

| OCSF class | Disposition | Status | Note |
|---|---|---|---|
| 0 `base_event` | Telemetry | built | the wrapper for a `query_logs` row no typed class fits (03 §2.7) |

### System Activity (16 classes)

| OCSF class | Disposition | Status | Note |
|---|---|---|---|
| 1001 `file_activity` | Telemetry | built | 03 §4.5 |
| 1002 `kernel_extension_activity` | Deferred | — | kernel extensions |
| 1003 `kernel_activity` | Deferred | — | kernel objects |
| 1004 `memory_activity` | Deferred | — | memory events; a sensor's memory detections are read through `search_alerts` |
| 1005 `module_activity` | Telemetry | built | 03 §4.8 |
| 1006 `scheduled_job_activity` | Telemetry | built | 03 §4.7 |
| 1007 `process_activity` | Telemetry | built | 03 §4.1 |
| 1008 `event_log_actvity` | Telemetry | proposed | log clearing and tampering: evidence the agent needs to explain a gap |
| 1009 `script_activity` | Telemetry | proposed | script content; today only the launching command line is read |
| 1010 `peripheral_activity` | Deferred | — | removable devices |
| 1011 `device_power_state_activity` | Out of scope | — | power state |
| 1012 `clipboard_activity` | Out of scope | — | clipboard |
| 201001 `registry_key_activity` | Telemetry | built | 03 §4.6 |
| 201002 `registry_value_activity` | Telemetry | built | 03 §4.6 |
| 201003 `windows_resource_activity` | Deferred | — | Windows resource access |
| 201004 `windows_service_activity` | Telemetry | proposed | service install and change, for `get_persistence_artifacts` |

### Findings (8 classes)

| OCSF class | Disposition | Status | Note |
|---|---|---|---|
| 2001 `security_finding` | Out of scope | — | superseded by 2004 |
| 2002 `vulnerability_finding` | Telemetry | proposed | for proposed `get_vulnerabilities` |
| 2003 `compliance_finding` | Deferred | — | compliance findings |
| 2004 `detection_finding` | Telemetry | built | 03 §4.12; how a source's own detections are read, through `search_alerts` |
| 2005 `incident_finding` | Telemetry | built | external cases (03 §2.9) |
| 2006 `data_security_finding` | Deferred | — | data-security findings |
| 2007 `application_security_posture_finding` | Deferred | — | application posture findings |
| 2008 `iam_analysis_finding` | Deferred | — | identity posture findings |

### Identity & Access Management (8 classes)

| OCSF class | Disposition | Status | Note |
|---|---|---|---|
| 3001 `account_change` | Telemetry | built | 03 §4.9 |
| 3002 `authentication` | Telemetry | built | 03 §4.2 |
| 3003 `authorize_session` | Telemetry | proposed | privileges granted to a session at logon |
| 3004 `entity_management` | Deferred | — | managed-entity changes |
| 3005 `user_access` | Telemetry | built | 03 §4.9 |
| 3006 `group_management` | Telemetry | built | 03 §4.9 |
| 3007 `user_management` | Deferred | — | user lifecycle |
| 3008 `role_management` | Deferred | — | role lifecycle |

### Network Activity (14 classes)

| OCSF class | Disposition | Status | Note |
|---|---|---|---|
| 4001 `network_activity` | Telemetry | built | 03 §4.3 |
| 4002 `http_activity` | Telemetry | proposed | for proposed `get_web_activity` |
| 4003 `dns_activity` | Telemetry | built | 03 §4.4 |
| 4004 `dhcp_activity` | Deferred | — | address leases; a host-identity input |
| 4005 `rdp_activity` | Telemetry | proposed | remote desktop sessions, for `get_lateral_movement_signals` |
| 4006 `smb_activity` | Telemetry | proposed | file-share sessions, for `get_lateral_movement_signals` |
| 4007 `ssh_activity` | Telemetry | proposed | remote shell sessions, for `get_lateral_movement_signals` |
| 4008 `ftp_activity` | Deferred | — | file transfer protocol |
| 4009 `email_activity` | Telemetry | built | 03 §4.10; the verb that reads it is specified, not built |
| 4010 `network_file_activity` | Deferred | — | network file activity |
| 4011 `email_file_activity` | Telemetry | built | 03 §4.11a; the verb that reads it is specified, not built |
| 4012 `email_url_activity` | Telemetry | built | 03 §4.11; the verb that reads it is specified, not built |
| 4013 `ntp_activity` | Out of scope | — | time sync |
| 4014 `tunnel_activity` | Deferred | — | tunnels |

### Discovery (26 classes)

| OCSF class | Disposition | Status | Note |
|---|---|---|---|
| 5001 `inventory_info` | Telemetry | built | 03 §4.17 |
| 5002 `config_state` | Telemetry | proposed | for proposed `get_configuration_state` |
| 5003 `user_inventory` | Telemetry | built | 03 §4.19 |
| 5004 `patch_state` | Telemetry | proposed | for proposed `get_vulnerabilities` |
| 5006 `kernel_object_query` | Deferred | — | live query: kernel objects |
| 5007 `file_query` | Telemetry | proposed | live query (§5.1) |
| 5008 `folder_query` | Deferred | — | live query: folders |
| 5009 `admin_group_query` | Telemetry | proposed | live query (§5.1) |
| 5010 `job_query` | Telemetry | proposed | live query (§5.1) |
| 5011 `module_query` | Telemetry | proposed | live query (§5.1) |
| 5012 `network_connection_query` | Telemetry | proposed | live query (§5.1) |
| 5013 `networks_query` | Deferred | — | live query: network interfaces |
| 5014 `peripheral_device_query` | Deferred | — | live query: peripherals |
| 5015 `process_query` | Telemetry | proposed | live query (§5.1) |
| 5016 `service_query` | Telemetry | proposed | live query (§5.1) |
| 5017 `session_query` | Telemetry | proposed | live query (§5.1) |
| 5018 `user_query` | Telemetry | proposed | live query (§5.1) |
| 5019 `device_config_state_change` | Deferred | — | device configuration changes |
| 5020 `software_info` | Telemetry | proposed | for proposed `get_software_inventory` |
| 5021 `osint_inventory_info` | Telemetry | proposed | threat-intel records, for `get_indicator_context` and proposed `get_threat_context` |
| 5022 `startup_item_query` | Telemetry | proposed | live query (§5.1) |
| 5023 `cloud_resources_inventory_info` | Telemetry | built | 03 §4.18 |
| 5040 `evidence_info` | Deferred | — | collected-evidence records; the engine records references to artifacts, not artifacts |
| 205004 `registry_key_query` | Telemetry | proposed | live query (§5.1) |
| 205005 `registry_value_query` | Telemetry | proposed | live query (§5.1) |
| 205019 `prefetch_query` | Deferred | — | live query: prefetch |

### Application Activity (8 classes)

| OCSF class | Disposition | Status | Note |
|---|---|---|---|
| 6001 `web_resources_activity` | Deferred | — | web resource changes |
| 6002 `application_lifecycle` | Deferred | — | application lifecycle |
| 6003 `api_activity` | Telemetry | built | 03 §4.15 |
| 6004 `web_resource_access_activity` | Deferred | — | web resource access |
| 6005 `datastore_activity` | Telemetry | proposed | data-store reads and writes: who read the objects in an exposed store |
| 6006 `file_hosting` | Deferred | — | file-hosting activity |
| 6007 `scan_activity` | Deferred | — | scanner runs |
| 6008 `application_error` | Out of scope | — | application errors |

### Remediation (4 classes)

| OCSF class | Disposition | Status | Note |
|---|---|---|---|
| 7001 `remediation_activity` | Telemetry | proposed | the record of a dispatched action (§5.2) |
| 7002 `file_remediation_activity` | Telemetry | proposed | as 7001 |
| 7003 `process_remediation_activity` | Telemetry | proposed | as 7001 |
| 7004 `network_remediation_activity` | Telemetry | proposed | as 7001 |

### Unmanned Systems (2 classes)

| OCSF class | Disposition | Status | Note |
|---|---|---|---|
| 8001 `drone_flights_activity` | Out of scope | — | unmanned systems |
| 8002 `airborne_broadcast_activity` | Out of scope | — | unmanned systems |

### 5.1 Live query classes

The Discovery category's `*_query` classes are the result shapes of a live
question put to a host: what is running, installed, scheduled or logged on
*now*. They need no new verbs. A live-response adapter binds the existing
ones (`get_process_executions`, `get_persistence_artifacts`,
`enumerate_logons`, `get_network_connections`, `get_file_activity`,
`get_privilege_changes`) and returns these classes where a log source returns
activity classes. Normalizing them is what lets one verb be answered from
history and from the live host.

### 5.2 Remediation classes

The Remediation category records that a response was carried out. The
engine's own record of a dispatched action is the `reckon-action` and its events
(`04 §3`, `08`). The classes are proposed as the shape in which a write
adapter's result, and a source's own report of a remediation, enter
telemetry, so that "was the persistence removed" is answered from an
observation and not from the action's status alone.

## 6. MITRE ATT&CK

ATT&CK is a label today: a hypothesis may carry technique ids (`01`), a
source's detection carries them on its indicator as external references
(`03 §4.12`), and SOPs and case summaries carry them for retrieval (`06`). The corpus is planned as
bundled reference data (`05 §11`). Its place in the universe is **Knowledge**,
with one structural link proposed: a technique resolves to the D3FEND
countermeasures that answer it, and from those to the action types this
tenant can dispatch (`04 §2.1` states the intent). That link is what carries
the agent from what happened to what to do.

Tactic-level coverage, by the verbs that observe each tactic:

| ATT&CK tactic | Observed through | Gap |
|---|---|---|
| Reconnaissance | not observable in tenant telemetry; `get_indicator_context` | — |
| Resource Development | not observable in tenant telemetry; proposed `get_threat_context` | — |
| Initial Access | `enumerate_logons`, `search_alerts`, `get_cloud_api_activity`; `get_email_messages` (specified) | the exploited weakness: proposed `get_vulnerabilities` |
| Execution | `get_process_executions`, `get_process_ancestry` | script content (OCSF 1009) |
| Persistence | `get_persistence_artifacts`, `get_privilege_changes` | service changes (OCSF 201004) |
| Privilege Escalation | `get_privilege_changes`, `get_process_executions` | session privileges (OCSF 3003) |
| Defense Evasion | `get_process_executions`, `get_file_activity` | log clearing (OCSF 1008) |
| Credential Access | `get_process_executions`, `get_file_activity`; `get_failed_auth_burst` (specified) | — |
| Discovery | `get_process_executions`, `get_cloud_api_activity` | — |
| Lateral Movement | `enumerate_logons`, `get_network_connections`; `get_lateral_movement_signals` (specified) | remote-session classes (OCSF 4005 to 4007) |
| Collection | `get_file_activity`, `get_cloud_api_activity` | data-store access (OCSF 6005) |
| Command and Control | `get_network_connections`, `resolve_dns`, `get_indicator_context` | web traffic (OCSF 4002) |
| Exfiltration | `get_network_connections` | web traffic and volumes: proposed `get_web_activity` |
| Impact | `get_file_activity`, `get_cloud_resource_history`, `search_alerts` | — |

Technique-level rows (which technique each verb can evidence) are not
enumerated in this pass.

## 7. Vulnerability, exposure and the standards beside the four

Nothing in the engine reads or records a vulnerability. The agent cannot ask
whether a host was exposed to the technique it sees, which other hosts carry
the same flaw, or whether a fix is in place. These are discriminating tests
for a hypothesis and the last step of a remediation.

| Standard | Disposition | Status | Use |
|---|---|---|---|
| CVE identifiers | Entity | proposed | the name of a STIX `vulnerability` |
| CVSS | Entity | proposed | severity, an attribute of the vulnerability observation |
| CPE | Entity | proposed | the product identifier on a STIX `software` |
| Known-exploited catalogues | Knowledge | proposed | whether a flaw is exploited in the wild, on `get_vulnerabilities` results |
| Exploit-likelihood scores (EPSS) | Deferred | — | a ranking aid, after the read exists |
| CWE | Deferred | — | weakness classes; of use to recommendations |
| Traffic Light Protocol | Export | specified | sharing markings (`07 §8.2`) |
| OpenC2 | Deferred | — | a standard wording for an action and its target; a candidate wire form for write adapters |
| CACAO | Deferred | — | a standard playbook form; a candidate export form for SOPs (`06`) |
| Detection rule formats (Sigma, YARA) | Deferred | — | wait on `detection.deploy` (`04 §2.1`, `07 §10`) |

## 8. What this pass proposes, and what it found

**Proposed reads** (each needs its own issue):

| Verb | Question | Standards it realizes |
|---|---|---|
| `get_vulnerabilities` | which known flaws does this host or product carry, and is the fix in place | D3-AVE, D3-NVA, D3-SYSVA, D3-SU; OCSF 2002, 5004; STIX `vulnerability` |
| `get_software_inventory` | what is installed on this host | D3-SWI; OCSF 5020; STIX `software` |
| `get_configuration_state` | how is this host configured now | D3-CI; OCSF 5002 |
| `get_threat_context` | which actor, malware or campaign is tied to this indicator or technique | STIX `threat-actor`, `intrusion-set`, `malware`, `tool`; OCSF 5021 |
| `get_file_analysis` | what does static or dynamic analysis say about this file | D3-FCOA, D3-FCR, D3-DA, D3-EFA; STIX `malware-analysis` |
| `get_web_activity` | which web requests did this host or user make | D3-WSAA, D3-UDTA; OCSF 4002 |

**Proposed actions:** none beyond the `04 §2.1` roadmap. Patching, enforcing
MFA, disabling a remote-access service and reissuing a certificate are
Handoffs.

**Proposed entities:** STIX `vulnerability`, `software`, `malware-analysis`;
the threat-side objects as Knowledge references.

**Findings and open questions:**

1. **What `search_alerts` cannot say.** 17 Detect techniques are read as a
   sensor's detections. `list_capabilities` reports whether the verb is
   bound, not which analyses the bound source runs. So an empty answer does
   not show that the analysis was made. Whether a source declares the analyses
   it runs is undecided.
2. **A D3FEND id on a read verb.** The action descriptor carries one
   (`04 §2.1`); the verb descriptor does not. A verb's descriptor text is
   model-facing and hashed into the prompt version, so the id must ride as
   metadata the model does not read, or the change moves every baseline.
3. **ATT&CK at technique level**, and the version it is pinned to.
4. **The technique-to-countermeasure link** (§6): a knowledge lookup, not an
   adapter verb; where it lives is undecided.
5. **STIX `opinion`.** `01` adopts it and nothing writes one. Either a use is
   named or `01` drops it.

## 9. Cross-references

`01` (entities, reasoning objects, STIX conformance), `03 §2` (verbs), `03 §4`
(normalizers), `03 §6.1` (coverage), `03 §7` (STIX deviations),
`04 §2.1` and `§2.2` (actions and the response-tactic dispositions), `06`
(knowledge), `07` (post-conclusion outputs), `08` (write side).
