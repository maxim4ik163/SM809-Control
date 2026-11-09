    .syntax unified
    .thumb
    .text
@ 0x41c00: side zone. Replaces "ldr r0,=0x4015f8 ; ldrb r0,[r0,#0x1c]" at 0x2dc74.
@ returns lighting mode, or 0 (side LEDs off, controller unpowered) when zone bit0 set
side:
    ldr   r0, CFG
    ldrb  r1, [r0, #0x1b]
    ldrb  r0, [r0, #0x1c]
    lsls  r1, r1, #31
    beq   1f
    movs  r0, #0
1:  bx    lr
@ 0x41c10: wheel / logo. Replaces "ldr r0,[sp,#0xc] ; cmp r0,#1" at 0x2d4d2.
@ r5 = LED buffer 0x40158c ([0..2] logo, [3..5] wheel); sp+0xc of caller unchanged.
leds:
    ldr   r3, CFG
    ldrb  r2, [r3, #0x1b]
    lsls  r1, r2, #30          @ bit1 -> N : wheel off
    bpl   2f
    movs  r1, #0
    strb  r1, [r5, #3]
    strb  r1, [r5, #4]
    strb  r1, [r5, #5]
2:  lsls  r1, r2, #29          @ bit2 -> N : logo = fixed colour (DPI colour slot 8)
    bpl   3f
    ldrb  r1, [r3, #0x0f]
    strb  r1, [r5, #0]
    ldrb  r1, [r3, #0x10]
    strb  r1, [r5, #1]
    ldrb  r1, [r3, #0x11]
    strb  r1, [r5, #2]
3:  ldr   r0, [sp, #0xc]
    cmp   r0, #1
    bx    lr
    .p2align 2
CFG: .word 0x4015f8
