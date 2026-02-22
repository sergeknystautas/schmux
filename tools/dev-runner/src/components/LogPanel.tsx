import React from 'react';
import { Box, Text, useStdout } from 'ink';

interface LogPanelProps {
  title: string;
  lines: string[];
  maxLines?: number;
}

export function LogPanel({ title, lines, maxLines }: LogPanelProps) {
  const { stdout } = useStdout();
  const termHeight = stdout?.rows ?? 24;
  // Reserve space for status bar (~5 lines) and key bar (1 line) and borders (2 lines)
  const availableLines = maxLines ?? Math.max(5, termHeight - 10);
  const visibleLines = lines.slice(-availableLines);

  return (
    <Box flexDirection="column" flexGrow={1} borderStyle="single" borderColor="gray">
      <Box>
        <Text bold color="cyan">{` ${title} `}</Text>
      </Box>
      <Box flexDirection="column" flexGrow={1}>
        {visibleLines.map((line, i) => (
          <Text key={i} wrap="truncate">
            {line}
          </Text>
        ))}
      </Box>
    </Box>
  );
}
