export type FleetDroneStatus =
  | 'idle'
  | 'en-route'
  | 'holding'
  | 'returning'
  | 'charging'
  | 'maintenance'
  | 'offline';

export type FleetMissionState = 'planned' | 'active' | 'paused' | 'completed' | 'aborted';

export interface FleetWaypoint {
  latitude: number;
  longitude: number;
}

export interface FleetDrone {
  id: string;
  model: string;
  status: FleetDroneStatus;
  battery: number;
  base: FleetWaypoint;
  latitude: number;
  longitude: number;
  firmware: string;
  missionId?: string;
}

export interface FleetMission {
  id: string;
  name: string;
  droneId: string;
  waypoints: FleetWaypoint[];
  state: FleetMissionState;
  progress: number;
}

export interface FleetSnapshot {
  drones: FleetDrone[];
  missions: FleetMission[];
}

export interface AddDroneRequest {
  id: string;
  model: string;
  base: FleetWaypoint;
  firmware: string;
}

export interface CreateMissionRequest {
  name: string;
  droneId: string;
  waypoints: FleetWaypoint[];
}

export type MissionAction = 'launch' | 'pause' | 'resume' | 'abort';

export const EMPTY_FLEET: FleetSnapshot = { drones: [], missions: [] };
