"use strict";
// Citations (02 §2.8): how a ref reads before and after the host resolves it,
// and which chip colour it takes (01 §Entity-type palette). Pure.
Object.defineProperty(exports, "__esModule", { value: true });
exports.ENTITY_POPOVER_TYPES = exports.REF_CHIP_CLASS = exports.REF_TYPE_LABELS = void 0;
exports.refType = refType;
exports.refTypeLabel = refTypeLabel;
exports.chipText = chipText;
exports.chipClass = chipClass;
exports.isEntityRef = isEntityRef;
exports.seedTypeLabel = seedTypeLabel;
/** Human type labels by STIX type prefix (an OCSF event id has no prefix). */
exports.REF_TYPE_LABELS = {
    "observed-data": "observed data", "x-host": "host", "ipv4-addr": "IP",
    "ipv6-addr": "IP", "user-account": "account", "process": "process",
    "file": "file", "domain-name": "domain", "email-addr": "email",
    "url": "URL", "windows-registry-key": "registry", "indicator": "indicator",
    "x-hypothesis": "hypothesis", "x-prediction": "prediction",
    "identity": "vendor", "sighting": "sighting",
    "note": "attestation", // a human statement recorded as evidence (adr/0054)
    "x-null-observation": "nothing observed", // a complete, empty answer recorded as evidence (adr/0061)
};
/** Entity-type palette classes: one fixed colour per type, everywhere. */
exports.REF_CHIP_CLASS = {
    "x-host": "chip-host", "ipv4-addr": "chip-ip", "ipv6-addr": "chip-ip",
    "user-account": "chip-account", "file": "chip-hash",
    "domain-name": "chip-domain", "url": "chip-domain", "email-addr": "chip-email",
    "indicator": "chip-alert", "sighting": "chip-alert",
    "x-hypothesis": "chip-brand", "x-prediction": "chip-brand",
};
/** Entity types get the popover (ui/02 §2.4); everything else opens its record. */
exports.ENTITY_POPOVER_TYPES = new Set([
    "x-host", "ipv4-addr", "ipv6-addr", "user-account", "process", "file",
    "domain-name", "email-addr", "url", "windows-registry-key", "indicator",
]);
/** The STIX type prefix of a ref, "" for an OCSF event id. */
function refType(ref) {
    return ref.includes("--") ? ref.slice(0, ref.indexOf("--")) : "";
}
function refTypeLabel(ref) {
    const t = refType(ref) || "event";
    return exports.REF_TYPE_LABELS[t] || t;
}
/** The chip text: type alone until resolved, then "type · value". */
function chipText(ref, resolved) {
    const type = refTypeLabel(ref);
    return resolved ? `${type} · ${resolved}` : type;
}
function chipClass(ref) {
    const c = exports.REF_CHIP_CLASS[refType(ref)];
    return "refchip" + (c ? " " + c : "");
}
function isEntityRef(ref) {
    return exports.ENTITY_POPOVER_TYPES.has(refType(ref));
}
/** The seed line's type word for an entity seed. */
function seedTypeLabel(ref) {
    const t = String(ref || "").split("--")[0];
    if (t === "x-host")
        return "host";
    if (t === "ipv4-addr" || t === "ipv6-addr")
        return "IP";
    if (t === "file")
        return "file";
    if (t === "user-account")
        return "account";
    if (t === "domain-name")
        return "domain";
    return "entity";
}
//# sourceMappingURL=refs.js.map