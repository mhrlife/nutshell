# Nutshell UI

## Scope
Voice-first coding assistant with a reading pane. This records the existing visual system and the session metadata toolbar change; it is not a site redesign.

## Direction
Quiet, compact, readable. Keep the answer primary. Group session identity beside the document actions rather than adding text to the conversation header.

## Existing tokens
Use style.css variables: paper, rail, line, ink-2, accent. Retain IBM Plex Sans, IBM Plex Mono and Vazirmatn. Metadata uses 12px text and 15px outline icons; expanded values use 12px monospace. Reuse the existing palette rather than introducing new colors.

## Toolbar
Three native details controls: terminal for agent, chip for model, folder for cwd. Show the folder basename at rest and the selectable full path on activation. Use 32px-high targets, 6px control corners, 8px popover corners, no animation or shadow. Full values and labels remain available to assistive technology. Preserve logical spacing and RTL layout. On narrow screens, wrap controls rather than overlap them.

## Audit
2026-09-16: Existing reader typography and palette retained. Keyboard focus, native touch activation, Escape dismissal, long-value wrapping and responsive layout covered by the implementation. Desktop and mobile toolbar renders checked in headless Chrome. Metadata aligns beside the document actions; narrow layouts wrap into two rows.
