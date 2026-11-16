const workOSOrganizationIDPattern = /^org_[A-Za-z0-9]+$/u;

export type WorkOSOrganizationID = string & { readonly __brand: "WorkOSOrganizationID" };

export function isWorkOSOrganizationID(input: string): input is WorkOSOrganizationID {
  return workOSOrganizationIDPattern.test(input);
}

export function parseOptionalWorkOSOrganizationID(
  input: string | undefined,
  name: string,
): WorkOSOrganizationID | undefined {
  if (input === undefined || input.length === 0) return undefined;
  if (!isWorkOSOrganizationID(input)) throw new Error(`${name} must be a WorkOS organization ID`);
  return input;
}
