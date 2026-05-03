/**
 * orbitron.h — Orbitron sovereign binary telemetry protocol
 *
 * ARM Cortex-M4 / bare-metal compatible, ANSI C99.
 * No heap allocation. No external dependencies.
 * Binary footprint: < 4 KB flash, < 512 B RAM.
 *
 * Protocol v2 wire layout:
 *   [16B header][1B presence][variable payload][32B HMAC-SHA256]
 *
 * HMAC-SHA256 is implemented as a software SHA-256 routine
 * (connect to hardware crypto unit if available on target).
 */

#ifndef ORBITRON_H
#define ORBITRON_H

#ifdef __cplusplus
extern "C" {
#endif

#include <stdint.h>
#include <stddef.h>
#include <stdbool.h>
#include <string.h>

/* ── Protocol constants ─────────────────────────────────────────────────── */

#define ORB_MAGIC            0x5A4C
#define ORB_VERSION          2

#define ORB_HEADER_SIZE      16u
#define ORB_PRESENCE_SIZE    1u
#define ORB_HMAC_SIZE        32u

/* Maximum frame size: 16 header + 1 presence + 35 payload + 32 HMAC */
#define ORB_MAX_FRAME_SIZE   96u

/* Presence bitmask */
#define ORB_PRESENCE_ATTITUDE        0x01u
#define ORB_PRESENCE_ANGULAR_VEL     0x02u
#define ORB_PRESENCE_POSITION        0x04u
#define ORB_PRESENCE_SOLAR_VOLTAGE   0x08u
#define ORB_PRESENCE_BATTERY_VOLTAGE 0x10u
#define ORB_PRESENCE_CHASSIS_TEMP    0x20u
#define ORB_PRESENCE_CPU_TEMP        0x40u
#define ORB_PRESENCE_RSSI            0x80u
#define ORB_PRESENCE_ALL             0xFFu

/* Flags byte */
#define ORB_FLAG_FULL_SYNC   0x01u
#define ORB_FLAG_CRIT_MODE   0x02u
#define ORB_FLAG_STORE_FWD   0x04u

/* Fixed-point scales (match Python side) */
#define ORB_SCALE_ANGLE      100
#define ORB_SCALE_ANG_VEL    1000
#define ORB_SCALE_LAT_LON    1000000
#define ORB_SCALE_ALT        10
#define ORB_SCALE_VOLTAGE    1000
#define ORB_SCALE_TEMP       10
#define ORB_SCALE_RSSI       10

/* Delta threshold: 1.5% relative change */
#define ORB_DELTA_THRESHOLD_NUM  15
#define ORB_DELTA_THRESHOLD_DEN  1000

/* ── Data types ──────────────────────────────────────────────────────────── */

typedef struct {
    float pitch;            /* degrees */
    float yaw;
    float roll;
    float omega_x;          /* rad/s */
    float omega_y;
    float omega_z;
    double latitude;        /* degrees (double for ~11 cm precision) */
    double longitude;
    double altitude;        /* metres */
    float solar_voltage;    /* volts */
    float battery_voltage;
    float chassis_temp;     /* °C */
    float cpu_temp;
    float rssi;             /* dBm */
} orb_state_t;

typedef struct {
    uint32_t seq;
    uint64_t timestamp_us;
    uint8_t  flags;
    uint8_t  presence;
    orb_state_t fields;     /* only populated fields are valid; check presence bits */
    bool     hmac_ok;
} orb_frame_t;

/* Error codes */
typedef enum {
    ORB_OK                = 0,
    ORB_ERR_BUF_TOO_SMALL = -1,
    ORB_ERR_MALFORMED     = -2,
    ORB_ERR_BAD_MAGIC     = -3,
    ORB_ERR_BAD_VERSION   = -4,
    ORB_ERR_HMAC_FAIL     = -5,
} orb_err_t;

/* ── Encoder ─────────────────────────────────────────────────────────────── */

/**
 * orb_encode() — build a Orbitron frame into `buf`.
 *
 * @param buf       output buffer (must be >= ORB_MAX_FRAME_SIZE bytes)
 * @param buf_len   length of buf
 * @param state     current telemetry values
 * @param prev      last acknowledged state (NULL forces full sync)
 * @param seq       monotonic packet counter
 * @param ts_us     current time in microseconds since Unix epoch
 * @param saturated true when link bandwidth < saturation threshold
 * @param hmac_key  pre-shared key for HMAC-SHA256 (may be NULL to skip auth)
 * @param key_len   length of hmac_key
 * @return          number of bytes written, or negative ORB_ERR_*
 */
int orb_encode(
    uint8_t       *buf,
    size_t         buf_len,
    const orb_state_t *state,
    const orb_state_t *prev,
    uint32_t       seq,
    uint64_t       ts_us,
    bool           saturated,
    const uint8_t *hmac_key,
    size_t         key_len
);

/**
 * orb_decode() — parse a received Orbitron frame.
 * Verifies HMAC if hmac_key != NULL.
 * Updates `mirror` in-place for every field present in the frame.
 *
 * @return ORB_OK, or negative ORB_ERR_*
 */
orb_err_t orb_decode(
    const uint8_t *buf,
    size_t         buf_len,
    orb_frame_t    *out,
    orb_state_t    *mirror,
    const uint8_t *hmac_key,
    size_t         key_len
);

/* ── Delta detection ──────────────────────────────────────────────────────── */

/**
 * orb_changed() — returns true if |new_val - old_val| / |old_val| > 1.5%
 * Handles zero-crossing: if old_val == 0, triggers when |new_val| > 1e-4.
 */
bool orb_changed(float new_val, float old_val);

/**
 * orb_compute_presence() — determine which fields to include based on delta.
 */
uint8_t orb_compute_presence(
    const orb_state_t *state,
    const orb_state_t *prev,
    bool              saturated
);

/* ── SHA-256 / HMAC (software implementation) ────────────────────────────── */

void orb_sha256(const uint8_t *msg, size_t len, uint8_t digest[32]);

void orb_hmac_sha256(
    const uint8_t *key,  size_t key_len,
    const uint8_t *msg,  size_t msg_len,
    uint8_t        tag[32]
);

/* ── Utility ──────────────────────────────────────────────────────────────── */

/** Big-endian write helpers */
static inline void orb_w16(uint8_t *p, uint16_t v) {
    p[0] = (uint8_t)(v >> 8); p[1] = (uint8_t)v;
}
static inline void orb_w32(uint8_t *p, uint32_t v) {
    p[0]=(uint8_t)(v>>24); p[1]=(uint8_t)(v>>16); p[2]=(uint8_t)(v>>8); p[3]=(uint8_t)v;
}
static inline void orb_w64(uint8_t *p, uint64_t v) {
    orb_w32(p,   (uint32_t)(v>>32));
    orb_w32(p+4, (uint32_t)(v));
}
static inline uint16_t orb_r16(const uint8_t *p) { return ((uint16_t)p[0]<<8)|p[1]; }
static inline uint32_t orb_r32(const uint8_t *p) {
    return ((uint32_t)p[0]<<24)|((uint32_t)p[1]<<16)|((uint32_t)p[2]<<8)|p[3];
}
static inline uint64_t orb_r64(const uint8_t *p) {
    return ((uint64_t)orb_r32(p)<<32)|(uint64_t)orb_r32(p+4);
}
static inline int16_t  orb_r16s(const uint8_t *p) { return (int16_t)orb_r16(p); }
static inline int32_t  orb_r32s(const uint8_t *p) { return (int32_t)orb_r32(p); }

#ifdef __cplusplus
}
#endif

#endif /* ORBITRON_H */
