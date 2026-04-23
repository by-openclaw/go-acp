import { LocaleData } from '../../../common/locale-data/locale-data.model';
import { IValidator } from '../../command-contract';
import { LocaleDataCache } from '../../command-locale-data-cache';
import { CommandErrorsKeys } from '../../locale-data-keys';
import { DestinationAssociationNamesResponseCommandOptions } from './options';
import { DestinationAssociationNamesResponseCommandParams } from './params';

/**
 * Destination Association Names Response Message command parameters validator
 *
 * @export
 * @class CommandParamsValidator
 * @implements {IValidator<DestinationAssociationNamesResponseCommandParams>}
 */
export class CommandParamsValidator implements IValidator<DestinationAssociationNamesResponseCommandParams> {
    /**
     * Creates an instance of CommandParamsValidator
     *
     * @param {DestinationAssociationNamesResponseCommandParams} data the command parameters
     * @param {DestinationAssociationNamesResponseCommandOptions} _options the command options (internally used)
     * @memberof CommandParamsValidator
     */
    constructor(
        readonly data: DestinationAssociationNamesResponseCommandParams,
        private readonly _options: DestinationAssociationNamesResponseCommandOptions
    ) {}

    /**
     * Validates the command parameter(s) and returns one error message for each invalid parameter
     *
     * @returns {Record<string, LocaleData>} the localized validation errors
     * @memberof CommandParamsValidator
     */
    validate(): Record<string, LocaleData> {
        const errors: Record<string, any> = {};
        const cache = LocaleDataCache.INSTANCE;

        if (this.isMatrixIdOutOfRange()) {
            errors.matrixId = cache.getCommandErrorLocaleData(CommandErrorsKeys.MATRIXID_IS_OUT_OF_RANGE_ERROR_MSG);
        }
        if (this.isLevelIdOutOfRange()) {
            errors.levelId = cache.getCommandErrorLocaleData(CommandErrorsKeys.LEVEL_IS_OUT_OF_RANGE_ERROR_MSG);
        }
        if (this.isDestinationIdAndMaximumNumberOfNamesOutOfRange()) {
            errors.destinationIdAndMaximumNumberOfNames = cache.getCommandErrorLocaleData(
                CommandErrorsKeys.DESTINATION_ID_AND_MAXIMUM_NUMBER_OF_NAMES_IS_OUT_OF_RANGE_ERROR_MSG
            );
        }
        if (this.isDestinationAssociationNameItemsOutOfRange()) {
            errors.destinationAssociationNameItems = cache.getCommandErrorLocaleData(
                CommandErrorsKeys.DESTINATION_ITEMS_IS_OUT_OF_RANGE_ERROR_MSG
            );
        }
        if (this.isFirstDestinationAssociationIdOutOfRange()) {
            errors.firstDestinationAssociationId = cache.getCommandErrorLocaleData(
                CommandErrorsKeys.DESTINATION_ASSOCIATION_IS_OUT_OF_RANGE_ERROR_MSG
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
    private isMatrixIdOutOfRange(): boolean {
        return this.data.matrixId < 0 || this.data.matrixId > 255;
    }

    /**
     * returns a boolean indicating if levelId is out of range
     * + false = levelId is included within the limits
     * + true = levelId [0-255] is out of range
     * @private
     * @returns {boolean} indicating if levelId is out of range
     * @memberof CommandParamsValidator
     */
    private isLevelIdOutOfRange(): boolean {
        return this.data.levelId < 0 || this.data.levelId > 255;
    }

    /**
     * returns a boolean indicating if levelId is out of range
     * + false = levelId is included within the limits
     * + true = levelId [0-255] is out of range
     * @private
     * @returns {boolean} indicating if levelId is out of range
     * @memberof CommandParamsValidator
     */
    private isFirstDestinationAssociationIdOutOfRange(): boolean {
        return this.data.firstDestinationAssociationId < 0 || this.data.firstDestinationAssociationId > 65535;
    }

    /**
     * returns a boolean indicating if sourceId + number of source Names to follow is out of range
     * + false = sourceId is included within the limits
     * + true = sourceId  [0-65535] is out of range
     * @private
     * @returns {boolean}
     * @memberof CommandParamsValidator
     */
    private isDestinationIdAndMaximumNumberOfNamesOutOfRange(): boolean {
        return (
            this.data.firstDestinationAssociationId + this.data.numberOfDestinationAssociationNamesToFollow < 0 ||
            this.data.firstDestinationAssociationId + this.data.numberOfDestinationAssociationNamesToFollow > 65535
        );
    }

    /**
     * Gets a boolean indicating whether the destinationAssociationNameItems is out of range
     * + false = destinationAssociationNameItems is included within the limits of the byteMaximumNumberOfNames
     * + true = destinationAssociationNameItems  [depend of the lengthOfSourceNamesReturned.byteLength] is out of range
     * @private
     * @returns {boolean} indicating if Source Names Items length is out of range
     * @memberof CommandParamsValidator
     */
    private isDestinationAssociationNameItemsOutOfRange(): boolean {
        if (this.data.destinationAssociationNameItems.length === 0) {
            return true;
        }
        for (let byteIndex = 0; byteIndex < this.data.destinationAssociationNameItems.length; byteIndex++) {
            const data = this.data.destinationAssociationNameItems[byteIndex];
            if (data.length < 1 || data.length > this._options.lengthOfDestinationAssociatonNamesReturned.byteLength) {
                return true;
            }
        }
        return false;
    }
}
