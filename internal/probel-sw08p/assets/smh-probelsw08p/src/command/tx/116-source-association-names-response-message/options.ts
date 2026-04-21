import { NameLength } from '../../shared/name-length';

/**
 * Source Names Response Message command Input option
 *
 * @export
 * @interface SourceAssociationNamesResponseCommandOptions
 */
export interface SourceAssociationNamesResponseCommandOptions {
    /**
     * Length of Names Required
     *
     * Len of names = 00
     * Length of Names Required : 4-char names
     *
     * Len of names = 01
     * Length of Names Required : 8-char names
     *
     * Len of names = 02
     * Length of Names Required : 12-char names
     *
     * @type {NameLength}
     * @memberof SourceAssociationNamesResponseCommandOptions
     */
    lengthOfNames: NameLength;
}

/**
 * Utility class providing some extra functionality on the SourceNamesResponseMessage command options
 *
 * @export
 * @class CommandOptionsUtility
 */
export class CommandOptionsUtility {
    /**
     * Gets a textual representation of the command options
     *
     * @param {SourceAssociationNamesResponseCommandOptions} options the command options
     * @returns {string} the command options textual representation
     * @memberof CommandOptionsUtility
     */
    static toString(options: SourceAssociationNamesResponseCommandOptions): string {
        return `Length of Source Names Returned:${this.toStringLengthOfNamesRequired(options.lengthOfNames)}`;
    }

    /**
     * Gets a textual representation of the NameLength
     *
     * @param {NameLength} data the NameLength
     * @returns {string} the NameLength textual representation
     * @memberof CommandOptionsUtility
     */
    static toStringLengthOfNamesRequired(data: NameLength): string {
        return `( ${data} - ${data.byteLength} )`;
    }
}
