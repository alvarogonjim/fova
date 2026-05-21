// Package assets owns every materializable, user-editable asset fova ships:
// config.toml, models.toml, the system prompt, and skills. All four
// materialize into Dir() on first run, are validated by one engine, and are
// editable and resettable through the /skills and /config TUI commands.
package assets
