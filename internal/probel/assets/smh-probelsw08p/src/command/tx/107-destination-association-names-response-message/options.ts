import { NameLength } from '../../shared/name-length';

/**
 * Destination Association Names Response Message
 *
 * @export
 * @interface DestinationAssociationNamesResponseCommandOptions
 */
export interface DestinationAssociationNamesResponseCommandOptions {
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
     * @memberof DestinationAssociationNamesResponseCommandOptions
     */
    lengthOfDestinationAssociatonNamesReturned: NameLength;
}

/**
 * Utility class providing some extra functionality on the DestinationsAssociationNamesRequestMessage command options
 *
 * @export
 * @class CommandOptionsUtility
 */
export class CommandOptionsUtility {
    /**
     * Gets a textual representation of the command options
     *
     * @param {DestinationAssociationNamesResponseCommandOptions} options the command options
     * @returns {string} the command options textual representation
     * @memberof CommandOptionsUtility
     */
    static toString(options: DestinationAssociationNamesResponseCommandOptions): string {
        return `Length of Destination Associaton Names Returned:${this.toStringLengthOfNamesRequired(
            options.lengthOfDestinationAssociatonNamesReturned
        )}`;
    }

    /**
     * Gets a textual representation of the NameLength
     *
     *
     * @param {NameLength} data the NameLength
     * @returns {string} the NameLength textual representation
     * @memberof CommandOptionsUtility
     */
    static toStringLengthOfNamesRequired(data: NameLength): string {
        return `( ${data} - ${data.byteLength} -  ${data.byteLength} - ${data.byteLength})`;
    }
}
