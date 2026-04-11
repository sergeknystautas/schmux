import React from 'react';
import { Box, Text } from 'ink';
import type { ProcessStatus } from '../types.js';

interface StatusBarProps {
  devRoot: string;
  workspace: string;
  backendStatus: ProcessStatus;
  frontendStatus: ProcessStatus;
  port: number;
  logLevel: string;
}

function statusDot(status: ProcessStatus): React.ReactNode {
  switch (status) {
    case 'running':
      return <Text color="green">●</Text>;
    case 'starting':
    case 'building':
    case 'pulling':
      return <Text color="yellow">●</Text>;
    case 'crashed':
    case 'stopped':
      return <Text color="red">●</Text>;
    case 'idle':
    default:
      return <Text dimColor>●</Text>;
  }
}

function statusLabel(status: ProcessStatus): string {
  switch (status) {
    case 'running':
      return 'running';
    case 'starting':
      return 'starting';
    case 'pulling':
      return 'pulling';
    case 'building':
      return 'building';
    case 'crashed':
      return 'crashed';
    case 'stopped':
      return 'stopped';
    case 'idle':
    default:
      return 'idle';
  }
}

export function StatusBar({
  devRoot,
  workspace,
  backendStatus,
  frontendStatus,
  port,
  logLevel,
}: StatusBarProps) {
  const isSameWorkspace = devRoot === workspace;
  const workspaceAnnotation = isSameWorkspace ? ' (same as dev root)' : ' (switched)';

  return (
    <Box
      flexDirection="column"
      borderStyle="single"
      borderBottom={false}
      borderColor="gray"
      paddingX={1}
    >
      <Text>
        <Text bold>schmux dev</Text>
      </Text>
      <Text>
        <Text dimColor>Dev root </Text>
        <Text>{devRoot}</Text>
      </Text>
      <Text>
        <Text dimColor>Workspace </Text>
        <Text>{workspace}</Text>
        <Text color={isSameWorkspace ? 'gray' : 'yellow'}>{workspaceAnnotation}</Text>
      </Text>
      <Text>
        <Text dimColor>Dashboard </Text>
        <Text color="cyan">{`http://localhost:${port}`}</Text>
      </Text>
      <Text>
        <Text>Backend </Text>
        {statusDot(backendStatus)}
        <Text> {statusLabel(backendStatus)} </Text>
        <Text>Frontend </Text>
        {statusDot(frontendStatus)}
        <Text> {statusLabel(frontendStatus)} </Text>
        <Text>Logs </Text>
        <Text color={logLevel === 'debug' ? 'yellow' : 'green'}>{logLevel}</Text>
      </Text>
    </Box>
  );
}
