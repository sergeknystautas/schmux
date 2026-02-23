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
  // Reserve space for StatusBar (6 rows), KeyBar (2 rows), and LogPanel border+title (3 rows)
  const availableLines = maxLines ?? Math.max(5, termHeight - 11);
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
