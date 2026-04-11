import React from 'react';
import { Box, Text } from 'ink';

interface KeyBarProps {
  canRestart: boolean;
  canResetWorkspace: boolean;
  layout?: 'horizontal' | 'vertical';
  plain?: boolean;
}

export function KeyBar({ canRestart, canResetWorkspace, layout, plain }: KeyBarProps) {
  if (plain) {
    return (
      <Box paddingX={1}>
        <Text>
          <Text bold dimColor={!canRestart} color={canRestart ? 'white' : undefined}>
            r
          </Text>
          <Text dimColor={!canRestart}> restart backend </Text>
          <Text bold dimColor={!canRestart} color={canRestart ? 'white' : undefined}>
            p
          </Text>
          <Text dimColor={!canRestart}> pull </Text>
          <Text bold dimColor={!canResetWorkspace} color={canResetWorkspace ? 'white' : undefined}>
            w
          </Text>
          <Text dimColor={!canResetWorkspace}> reset workspace </Text>
          <Text bold>q</Text>
          <Text> quit</Text>
        </Text>
      </Box>
    );
  }

  return (
    <Box borderStyle="single" borderTop={false} borderColor="gray" paddingX={1}>
      <Text>
        <Text bold dimColor={!canRestart} color={canRestart ? 'white' : undefined}>
          r
        </Text>
        <Text dimColor={!canRestart}> restart backend </Text>
        <Text bold dimColor={!canRestart} color={canRestart ? 'white' : undefined}>
          p
        </Text>
        <Text dimColor={!canRestart}> pull </Text>
        <Text bold dimColor={!canResetWorkspace} color={canResetWorkspace ? 'white' : undefined}>
          w
        </Text>
        <Text dimColor={!canResetWorkspace}> reset workspace </Text>
        <Text bold>c</Text>
        <Text> clear logs </Text>
        <Text bold>l</Text>
        <Text> {layout === 'horizontal' ? 'stack' : 'split'} logs </Text>
        <Text bold>q</Text>
        <Text> quit</Text>
      </Text>
    </Box>
  );
}
