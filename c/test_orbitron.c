/**
 * Minimal round-trip test for the Orbitron C library.
 * Build: gcc -O2 -Wall -Wextra -o test_orbitron test_orbitron.c orbitron.c -lm
 * Run:   ./test_orbitron
 */

#include <stdio.h>
#include <assert.h>
#include <math.h>
#include "orbitron.h"

static const uint8_t TEST_KEY[] = "orbitron-dev-key-2024";
#define KEY_LEN (sizeof(TEST_KEY)-1)

static void test_hmac(void)
{
    /* RFC 4231 test vector #1 */
    const uint8_t key[20] = {0x0b,0x0b,0x0b,0x0b,0x0b,0x0b,0x0b,0x0b,0x0b,0x0b,
                              0x0b,0x0b,0x0b,0x0b,0x0b,0x0b,0x0b,0x0b,0x0b,0x0b};
    const uint8_t msg[]   = "Hi There";
    const uint8_t expect[32] = {
        0xb0,0x34,0x4c,0x61,0xd8,0xdb,0x38,0x53,0x5c,0xa8,0xaf,0xce,0xaf,0x0b,0xf1,0x2b,
        0x88,0x1d,0xc2,0x00,0xc9,0x83,0x3d,0xa7,0x26,0xe9,0x37,0x6c,0x2e,0x32,0xcf,0xf7,
    };
    uint8_t tag[32];
    orb_hmac_sha256(key, 20, msg, 8, tag);
    assert(memcmp(tag, expect, 32) == 0 && "HMAC-SHA256 RFC4231 vector failed");
    printf("  [PASS] HMAC-SHA256 RFC4231 test vector\n");
}

static void test_round_trip(void)
{
    orb_state_t state = {
        .pitch   = 12.47f, .yaw = 83.22f, .roll = -3.18f,
        .omega_x = 0.014f, .omega_y = 0.035f, .omega_z = 0.007f,
        .latitude = -1.2864, .longitude = 36.8172, .altitude = 551234.0,
        .solar_voltage = 28.47f, .battery_voltage = 23.84f,
        .chassis_temp = 22.3f, .cpu_temp = 45.7f, .rssi = -87.4f,
    };

    uint8_t buf[ORB_MAX_FRAME_SIZE];
    int len = orb_encode(buf, sizeof(buf), &state, NULL, 42000,
                        1700000000ULL * 1000000ULL, false,
                        TEST_KEY, KEY_LEN);
    assert(len > 0 && "encode failed");
    printf("  [PASS] Encode full-sync frame: %d bytes\n", len);

    orb_state_t mirror = {0};
    orb_frame_t frame  = {0};
    orb_err_t err = orb_decode(buf, (size_t)len, &frame, &mirror, TEST_KEY, KEY_LEN);
    assert(err == ORB_OK      && "decode failed");
    assert(frame.hmac_ok     && "HMAC verification failed");
    assert(frame.presence == ORB_PRESENCE_ALL && "presence mismatch");
    assert(fabsf(mirror.pitch - state.pitch) < 0.02f && "pitch round-trip error");
    assert(fabs(mirror.latitude - state.latitude) < 0.000002 && "lat round-trip error");
    printf("  [PASS] Decode + HMAC verify, presence=0x%02X\n", frame.presence);

    /* Tamper with HMAC — should return ORB_ERR_HMAC_FAIL */
    buf[len - 1] ^= 0xFF;
    err = orb_decode(buf, (size_t)len, &frame, &mirror, TEST_KEY, KEY_LEN);
    assert(err == ORB_ERR_HMAC_FAIL && "expected HMAC failure");
    printf("  [PASS] Tampered frame correctly rejected (ORB_ERR_HMAC_FAIL)\n");
}

static void test_delta(void)
{
    orb_state_t prev = {.pitch=10.0f, .rssi=-90.0f, .battery_voltage=24.0f};
    orb_state_t curr = {.pitch=10.0f, .rssi=-88.0f, .battery_voltage=24.1f};

    uint8_t pres = orb_compute_presence(&curr, &prev, false);
    /* pitch unchanged (< 1.5%), rssi changed (2.2%), battery changed (0.4% — below threshold) */
    assert(!(pres & ORB_PRESENCE_ATTITUDE)        && "pitch should NOT be present");
    assert(  (pres & ORB_PRESENCE_RSSI)           && "rssi SHOULD be present");
    printf("  [PASS] Delta presence=0x%02X (rssi changed, pitch stable)\n", pres);
}

int main(void)
{
    printf("Orbitron C library tests\n");
    printf("─────────────────────────────────────────────\n");
    test_hmac();
    test_round_trip();
    test_delta();
    printf("─────────────────────────────────────────────\n");
    printf("All tests passed.\n");
    return 0;
}
