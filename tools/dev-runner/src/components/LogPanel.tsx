import React from 'react';
import { Box, Text, useStdout } from 'ink';

interface LogPanelProps {
  title: string;
  lines: string[];
  maxLines?: number;
  layout?: 'horizontal' | 'vertical';
  flex?: number;
}

export function LogPanel({ title, lines, maxLines, layout = 'horizontal', flex = 1 }: LogPanelProps) {
  const { stdout } = useStdout();
  const termHeight = stdout?.rows ?? 24;
  // Reserve space for StatusBar (6 rows), KeyBar (2 rows), and LogPanel border+title (3 rows per panel)
  // Horizontal: one row of panels, each gets full height minus chrome
  // Vertical: two stacked panels split the remaining height
  let defaultLines: number;
  if (layout === 'vertical') {
    // Two panels stacked: subtract StatusBar(6) + KeyBar(2) + 2×border+title(3) = 14
    defaultLines = Math.max(3, Math.floor((termHeight - 14) / 2));
  } else {
    defaultLines = Math.max(5, termHeight - 11);
  }
  const availableLines = maxLines ?? defaultLines;
  const visibleLines = lines.slice(-availableLines);

  return (
    <Box flexDirection="column" flexGrow={flex} borderStyle="single" borderColor="gray">
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
