import React from 'react';
import { Box, Text } from 'ink';

interface KeyBarProps {
  canRestart: boolean;
}

export function KeyBar({ canRestart }: KeyBarProps) {
  return (
    <Box borderStyle="single" borderTop={false} borderColor="gray" paddingX={1}>
      <Text>
        <Text bold dimColor={!canRestart} color={canRestart ? 'white' : undefined}>
          r
        </Text>
        <Text dimColor={!canRestart}> restart backend </Text>
        <Text bold>c</Text>
        <Text> clear logs </Text>
        <Text bold>q</Text>
        <Text> quit</Text>
      </Text>
    </Box>
  );
}
