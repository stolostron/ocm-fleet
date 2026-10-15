// Perry needs this explicit import to lower JSX as React elements, not eager calls.
import "react/jsx-runtime";

import { Box, Text } from "ink";

import type { CheckResult } from "../contracts";

// Perry currently drops JSX's special `key` attribute. Spreading it as a normal
// prop lets React.createElement retain keys in its lowered children arrays.

export type MessageStyle =
  "banner" | "heading" | "success" | "warning" | "error" | "muted";

const tones = {
  pass: "green",
  warning: "yellow",
  fail: "red",
  error: "red",
} as const;

export function Banner({ color }: { color: boolean }) {
  return (
    <Box
      flexDirection="column"
      borderStyle="round"
      borderColor={color ? "cyan" : undefined}
      paddingX={1}
    >
      <Text
        {...{ key: "brand" }}
        color={color ? "cyan" : undefined}
        bold={color}
      >
        OME / FleetShift
      </Text>
      <Text {...{ key: "title" }} bold={color}>
        AIO installation check
      </Text>
      <Text {...{ key: "subtitle" }} dimColor={color}>
        Local host readiness · guided setup
      </Text>
    </Box>
  );
}

export function Message({
  message,
  style,
  color,
}: {
  message: string;
  style?: MessageStyle;
  color: boolean;
}) {
  if (style === "banner") return <Banner color={color} />;
  const tone =
    style === "success"
      ? "green"
      : style === "warning"
        ? "yellow"
        : style === "error"
          ? "red"
          : style === "heading"
            ? "cyan"
            : undefined;
  return (
    <Text
      color={color ? tone : undefined}
      bold={color && style === "heading"}
      dimColor={color && style === "muted"}
    >
      {message}
    </Text>
  );
}

export function CheckPanel({
  result,
  color,
}: {
  result: CheckResult;
  color: boolean;
}) {
  const tone = tones[result.status];
  return (
    <Box flexDirection="column">
      <Text {...{ key: "title" }} color={color ? tone : undefined} bold={color}>
        {`${result.status.toUpperCase()}  ${result.title}${result.blocking && result.status !== "pass" ? " — required fix" : ""}`}
      </Text>
      <Box {...{ key: "detail" }} paddingLeft={2}>
        <Text dimColor={color && result.status === "pass"}>
          {result.reason}
        </Text>
      </Box>
      {result.remediation && (
        <Box {...{ key: "remediation" }} flexDirection="column" marginTop={1}>
          <Text
            wrap="truncate-end"
            {...{ key: "explanation" }}
            color={color ? "yellow" : undefined}
          >
            Fix guidance: {result.remediation.explanation}
          </Text>
          <Text {...{ key: "documentation" }} dimColor={color}>
            Documentation:{" "}
            {result.remediation.documentationUrl ??
              "Documentation link unavailable"}
          </Text>
          {(result.remediation.actions ?? []).map((action) => (
            <Box {...{ key: action.id }} flexDirection="column">
              <Text {...{ key: "description" }}>
                {action.title} — {action.description}
              </Text>
              {action.command && (
                <Text {...{ key: "command" }}>{action.command}</Text>
              )}
            </Box>
          ))}
        </Box>
      )}
    </Box>
  );
}

export function Selection({
  choices,
  focused,
  color,
  rows,
}: {
  choices: string[];
  focused: number;
  color: boolean;
  rows: number;
}) {
  // Keep live region shorter than viewport: Ink otherwise clears terminal history.
  // Full labels are printed before mounting; live labels use one line each.
  const count = Math.max(1, Math.min(choices.length, rows - 5));
  const start = Math.max(
    0,
    Math.min(focused - Math.floor(count / 2), choices.length - count),
  );
  if (rows < 7)
    return (
      <Box height={1} overflow="hidden">
        <Text>❯ {choices[focused]}</Text>
      </Box>
    );
  return (
    <Box
      flexDirection="column"
      borderStyle="round"
      borderColor={color ? "cyan" : undefined}
      paddingX={1}
    >
      {choices.slice(start, start + count).map((choice, index) => (
        <Box {...{ key: start + index }} height={1} overflow="hidden">
          <Text
            color={color && focused === start + index ? "cyan" : undefined}
            bold={color && focused === start + index}
          >
            {focused === start + index ? "❯ " : "  "}
            {choice}
          </Text>
        </Box>
      ))}
      <Box {...{ key: "hint" }} height={1} overflow="hidden">
        <Text wrap="truncate-end" dimColor={color}>
          ↑/↓ move · Enter confirm · Ctrl-C cancel
        </Text>
      </Box>
    </Box>
  );
}
