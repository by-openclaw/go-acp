import { LocaleData } from '../../../common/locale-data/locale-data.model';
import { IValidator } from '../../command-contract';
import { LocaleDataCache } from '../../command-locale-data-cache';
import { CommandErrorsKeys } from '../../locale-data-keys';
import { CrossPointTieLineTallyCommandParams } from './params';

/**
 * Source Names Response Message Validator
 *
 * @export
 * @class CommandParamsValidator
 * @implements {IValidator<CrossPointTieLineTallyCommandParams>}
 */
export class CommandParamsValidator implements IValidator<CrossPointTieLineTallyCommandParams> {
    /**
     * Creates an instance of CommandParamsValidator
     *
     * @param {CrossPointTieLineTallyCommandParams} data the command parameters
     * @memberof CommandParamsValidator
     */
    constructor(readonly data: CrossPointTieLineTallyCommandParams) {}

    /**
     * Validates the command parameter(s) and returns one error message for each invalid parameter
     *
     * @returns {Record<string, LocaleData>} the localized validation errors
     * @memberof CommandParamsValidator
     */
    validate(): Record<string, LocaleData> {
        const errors: Record<string, any> = {};
        const cache = LocaleDataCache.INSTANCE;

        if (this.isDestinationMatrixIdOutOfRange()) {
            errors.destinationMatrixId = cache.getCommandErrorLocaleData(
                CommandErrorsKeys.DESTINATION_MATRIXID_IS_OUT_OF_RANGE_ERROR_MSG
            );
        }
        if (this.isDestinationAssociationOutOfRange()) {
            errors.destinationAssociation = cache.getCommandErrorLocaleData(
                CommandErrorsKeys.DESTINATION_ASSOCIATION_IS_OUT_OF_RANGE_ERROR_MSG
            );
        }
        if (this.isNumberOfSourcesReturnedOutOfRange()) {
            errors.numberOfSourcesReturned = cache.getCommandErrorLocaleData(
                CommandErrorsKeys.NUMBER_OF_SOURCES_RETURNED_IS_OUT_OF_RANGE_ERROR_MSG
            );
        }
        if (this.isSourceItemsOutOfRange()) {
            errors.sourceItems = cache.getCommandErrorLocaleData(
                CommandErrorsKeys.SOURCE_ITEMS_IS_OUT_OF_RANGE_ERROR_MSG
            );
        }
        return errors;
    }

    /**
     * returns a boolean indicating if matrixId is out of range
     * + false = matrixId is included within the limits
     * + true = matrixId [0-255] is out of range
     * @private
     * @returns {boolean} indicating if matrixId is out of range
     * @memberof CommandParamsValidator
     */
    private isDestinationMatrixIdOutOfRange(): boolean {
        return this.data.destinationMatrixId < 0 || this.data.destinationMatrixId > 19;
    }

    /**
     * returns a boolean indicating if levelId is out of range
     * + false = levelId is included within the limits
     * + true = levelId [0-255] is out of range
     * @private
     * @returns {boolean} indicating if levelId is out of range
     * @memberof CommandParamsValidator
     */
    private isDestinationAssociationOutOfRange(): boolean {
        return this.data.destinationAssociation < 0 || this.data.destinationAssociation > 65535;
    }

    /**
     * returns a boolean indicating if numberOfTalliesReturned is out of range
     * + false = numberOfTalliesReturned is included within the limits
     * + true = numberOfTalliesReturned  [1-64] is out of range
     * @private
     * @returns {boolean} indicating if numberOfTalliesReturned is out of range
     * @memberof export class CrossPointConnectedMessageCommandParamsValidator

     */
    private isNumberOfSourcesReturnedOutOfRange(): boolean {
        return this.data.numberOfSourcesReturned < 1 || this.data.numberOfSourcesReturned > 256;
    }

    /**
     * returns a boolean indicating if sourceItems is out of range
     * + false = sourceItems is included within the limits
     * + true = sourceItems is out of range
     * @private
     * @returns {boolean}
     * @memberof CommandParamsValidator
     */
    private isSourceItemsOutOfRange(): boolean {
        if (this.data.sourceItems.length === 0) {
            return true;
        }
        if (this.data.sourceItems.length > 256) {
            return true;
        }
        for (let byteIndex = 0; byteIndex < this.data.sourceItems.length; byteIndex++) {
            const data = this.data.sourceItems[byteIndex];
            if (data.sourceMatrixId < 0 || data.sourceMatrixId > 255) {
                return true;
            }
            if (data.sourceLevel < 0 || data.sourceLevel > 255) {
                return true;
            }
            if (data.sourceId < 0 || data.sourceId > 65535) {
                return true;
            }
        }
        return false;
    }
}
