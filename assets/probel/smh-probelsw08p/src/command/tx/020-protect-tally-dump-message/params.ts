import { ProtectTallyDumpCommandItems } from './items';

/**
 * Protect Tally Dump Message command params
 *
 * @export
 * @interface ProtectTallyDumpCommandParams
 */
export interface ProtectTallyDumpCommandParams {
    /**
     *
     * Matrix number
     * @type {number}
     * @memberof ProtectTallyDumpCommandParams
     */
    matrixId: number;

    /**
     *
     * Level number
     * @type {number}
     * @memberof ProtectTallyDumpCommandParams
     */
    levelId: number;

    /**
     * Number of tallies returned (Max 64)
     * Tallies number
     * @type {number}
     * @memberof ProtectTallyDumpCommandParams
     */
    numberOfProtectTallies: number;

    /**
     *
     * First Destination number
     * @type {number}
     * @memberof ProtectTallyDumpCommandParams
     */
    firstDestinationId: number;

    /**
     *
     *
     * @type {ProtectTallyDumpCommandItems[]}
     * @memberof ProtectTallyDumpCommandParams
     */
    deviceNumberProtectDataItems: ProtectTallyDumpCommandItems[];
}

/**
 * Utility class providing extra functionalities on the ProtectTallyDump command parameters
 *
 * @export
 * @class CommandParamsUtility
 */
export class CommandParamsUtility {
    /**
     * Gets a textual representation of the command parameters
     *
     * @static
     * @param {ProtectTallyDumpCommandParams} params the command parameters
     * @returns {string} the command parameters textual representation
     * @memberof CommandParamsUtility
     */
    static toString(params: ProtectTallyDumpCommandParams): string {
        // TODO : Add deviceNumberProtectDataItems[]
        return `Matrix:${params.matrixId}, Level:${params.levelId}, Number Of Protect Tallies:${params.numberOfProtectTallies}, First Destination:${params.firstDestinationId}, deviceNumberProtectDataItems[]`;
    }
}
