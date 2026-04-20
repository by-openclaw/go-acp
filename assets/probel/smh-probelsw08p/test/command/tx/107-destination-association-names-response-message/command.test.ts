import * as _ from 'lodash';
import { DestinationAssociationNamesResponseCommand } from '../../../../src/command/tx/107-destination-association-names-response-message/command';
import { DestinationAssociationNamesResponseCommandParams } from '../../../../src/command/tx/107-destination-association-names-response-message/params';
import { Fixture } from '../../../fixture/fixture';
import { BootstrapService } from '../../../../src/bootstrap.service';
import { ValidationError, CommandIdentifiers, CommandIdentifier } from '../../../../src/command/command-contract';
import { CommandErrorsKeys } from '../../../../src/command/locale-data-keys';
import {
    DestinationAssociationNamesResponseCommandOptions
} from '../../../../src/command/tx/107-destination-association-names-response-message/options';
import { NameLength } from '../../../../src/command/shared/name-length';

describe('Destination Association Names Response Message', () => {
    beforeAll(async () => {
        await BootstrapService.bootstrapAsync('en');
    });

    describe('ctor', () => {
        it('Should instantiate the command with valid params, options', () => {
            // Arrange
            const params: DestinationAssociationNamesResponseCommandParams = {
                matrixId: 0,
                levelId: 0,
                firstDestinationAssociationId: 0,
                numberOfDestinationAssociationNamesToFollow: 32,
                destinationAssociationNameItems: Fixture.buildCharItems(64, 4)
            };
            const options: DestinationAssociationNamesResponseCommandOptions = {
                lengthOfDestinationAssociatonNamesReturned: NameLength.FOUR_CHAR_NAMES
            };
            // Act
            const command = new DestinationAssociationNamesResponseCommand(params, options);

            // Assert
            expect(command).toBeDefined();
            expect(command.params).toBe(params);
            expect(command.options).toBe(options);
            expect(command.identifier).toBe(CommandIdentifiers.TX.GENERAL.DESTINATION_ASSOCIATION_NAMES_RESPONSE_MESSAGE);
        });

        it('Should throw an error if params params are out of range < MIN', done => {
            // Arrange
            const params: DestinationAssociationNamesResponseCommandParams = {
                matrixId: -1,
                levelId: -1,
                firstDestinationAssociationId: -1,
                numberOfDestinationAssociationNamesToFollow: 0,
                destinationAssociationNameItems: []
            };
            const options: DestinationAssociationNamesResponseCommandOptions = {
                lengthOfDestinationAssociatonNamesReturned: NameLength.SIXTEEN_CHAR_NAMES
            };
            // Act
            const fct = () => new DestinationAssociationNamesResponseCommand(params, options);

            // Assert
            try {
                fct();
            } catch (e) {
                expect(e).toBeInstanceOf(ValidationError);
                const localeDataError = e as ValidationError;
                expect(localeDataError.validationErrors).toBeDefined();
                expect(localeDataError.message).toBeDefined();
                expect(localeDataError.validationErrors?.matrixId.id).toBe(
                    CommandErrorsKeys.MATRIXID_IS_OUT_OF_RANGE_ERROR_MSG
                );
                expect(localeDataError.validationErrors?.levelId.id).toBe(
                    CommandErrorsKeys.LEVEL_IS_OUT_OF_RANGE_ERROR_MSG
                );
                expect(localeDataError.validationErrors?.firstDestinationAssociationId.id).toBe(
                    CommandErrorsKeys.DESTINATION_ASSOCIATION_IS_OUT_OF_RANGE_ERROR_MSG
                );
                expect(localeDataError.validationErrors?.destinationIdAndMaximumNumberOfNames.id).toBe(
                    CommandErrorsKeys.DESTINATION_ID_AND_MAXIMUM_NUMBER_OF_NAMES_IS_OUT_OF_RANGE_ERROR_MSG
                );
                expect(localeDataError.validationErrors?.destinationAssociationNameItems.id).toBe(
                    CommandErrorsKeys.DESTINATION_ITEMS_IS_OUT_OF_RANGE_ERROR_MSG
                );
                done();
            }
        });

        it('Should throw an error if params params are out of range > MAX', done => {
            // Arrange
            const params: DestinationAssociationNamesResponseCommandParams = {
                matrixId: 256,
                levelId: 256,
                firstDestinationAssociationId: 65536,
                numberOfDestinationAssociationNamesToFollow: 32,
                destinationAssociationNameItems: []
            };
            const options: DestinationAssociationNamesResponseCommandOptions = {
                lengthOfDestinationAssociatonNamesReturned: NameLength.SIXTEEN_CHAR_NAMES
            };
            // Act
            const fct = () => new DestinationAssociationNamesResponseCommand(params, options);

            // Assert
            try {
                fct();
            } catch (e) {
                expect(e).toBeInstanceOf(ValidationError);
                const localeDataError = e as ValidationError;
                expect(localeDataError.validationErrors).toBeDefined();
                expect(localeDataError.message).toBeDefined();
                expect(localeDataError.validationErrors?.matrixId.id).toBe(
                    CommandErrorsKeys.MATRIXID_IS_OUT_OF_RANGE_ERROR_MSG
                );
                expect(localeDataError.validationErrors?.levelId.id).toBe(
                    CommandErrorsKeys.LEVEL_IS_OUT_OF_RANGE_ERROR_MSG
                );
                expect(localeDataError.validationErrors?.firstDestinationAssociationId.id).toBe(
                    CommandErrorsKeys.DESTINATION_ASSOCIATION_IS_OUT_OF_RANGE_ERROR_MSG
                );
                expect(localeDataError.validationErrors?.destinationIdAndMaximumNumberOfNames.id).toBe(
                    CommandErrorsKeys.DESTINATION_ID_AND_MAXIMUM_NUMBER_OF_NAMES_IS_OUT_OF_RANGE_ERROR_MSG
                );
                expect(localeDataError.validationErrors?.destinationAssociationNameItems.id).toBe(
                    CommandErrorsKeys.DESTINATION_ITEMS_IS_OUT_OF_RANGE_ERROR_MSG
                );
                done();
            }
        });
    });

    describe('buildCommand', () => {
        describe('General Destination Association Names Response Message CMD_107_0X6b', () => {
            // let commandIdentifier: CommandIdentifier;

            // beforeAll(() => {
            //     commandIdentifier = CommandIdentifiers.TX.GENERAL.SOURCE_NAMES_RESPONSE_MESSAGE;
            // });

            it('Should create & pack the general command (...)', () => {
                // Arrange
                const params: DestinationAssociationNamesResponseCommandParams = {
                    matrixId: 0,
                    levelId: 0,
                    firstDestinationAssociationId: 0,
                    numberOfDestinationAssociationNamesToFollow: 32,
                    destinationAssociationNameItems: Fixture.buildCharItems(64, 4)
                };
                const options: DestinationAssociationNamesResponseCommandOptions = {
                    lengthOfDestinationAssociatonNamesReturned: NameLength.FOUR_CHAR_NAMES
                };
                let commandIdentifier: CommandIdentifier;
                commandIdentifier = CommandIdentifiers.TX.GENERAL.DESTINATION_ASSOCIATION_NAMES_RESPONSE_MESSAGE;
                // Act
                const metaCommand = new DestinationAssociationNamesResponseCommand(params, options);
                metaCommand.buildCommand();

                // Assert
                Fixture.assertMetaCommand(metaCommand, commandIdentifier, [
                    {
                        data:
                            '6b 00 00 00 00 20 30 30 30 30 30 30 30 31 30 30 30 32 30 30 30 33 30 30 30 34 30 30 30 35 30 30 30 36 30 30 30 37 30 30 30 38 30 30 30 39 30 30 31 30 30 30 31 31 30 30 31 32 30 30 31 33 30 30 31 34 30 30 31 35 30 30 31 36 30 30 31 37 30 30 31 38 30 30 31 39 30 30 32 30 30 30 32 31 30 30 32 32 30 30 32 33 30 30 32 34 30 30 32 35 30 30 32 36 30 30 32 37 30 30 32 38 30 30 32 39 30 30 33 30 30 30 33 31',
                        bytesCount: 0x86,
                        checksum: 0x43,
                        buffer:
                            '10 02 6b 00 00 00 00 20 30 30 30 30 30 30 30 31 30 30 30 32 30 30 30 33 30 30 30 34 30 30 30 35 30 30 30 36 30 30 30 37 30 30 30 38 30 30 30 39 30 30 31 30 30 30 31 31 30 30 31 32 30 30 31 33 30 30 31 34 30 30 31 35 30 30 31 36 30 30 31 37 30 30 31 38 30 30 31 39 30 30 32 30 30 30 32 31 30 30 32 32 30 30 32 33 30 30 32 34 30 30 32 35 30 30 32 36 30 30 32 37 30 30 32 38 30 30 32 39 30 30 33 30 30 30 33 31 86 43 10 03'
                    },
                    {
                        data:
                            '6b 00 00 00 20 20 30 30 33 32 30 30 33 33 30 30 33 34 30 30 33 35 30 30 33 36 30 30 33 37 30 30 33 38 30 30 33 39 30 30 34 30 30 30 34 31 30 30 34 32 30 30 34 33 30 30 34 34 30 30 34 35 30 30 34 36 30 30 34 37 30 30 34 38 30 30 34 39 30 30 35 30 30 30 35 31 30 30 35 32 30 30 35 33 30 30 35 34 30 30 35 35 30 30 35 36 30 30 35 37 30 30 35 38 30 30 35 39 30 30 36 30 30 30 36 31 30 30 36 32 30 30 36 33',
                        bytesCount: 0x86,
                        checksum: 0xb9,
                        buffer:
                            '10 02 6b 00 00 00 20 20 30 30 33 32 30 30 33 33 30 30 33 34 30 30 33 35 30 30 33 36 30 30 33 37 30 30 33 38 30 30 33 39 30 30 34 30 30 30 34 31 30 30 34 32 30 30 34 33 30 30 34 34 30 30 34 35 30 30 34 36 30 30 34 37 30 30 34 38 30 30 34 39 30 30 35 30 30 30 35 31 30 30 35 32 30 30 35 33 30 30 35 34 30 30 35 35 30 30 35 36 30 30 35 37 30 30 35 38 30 30 35 39 30 30 36 30 30 30 36 31 30 30 36 32 30 30 36 33 86 b9 10 03'
                    }
                ]);
            });

            it('Should create & pack the extended command (...)', () => {
                // Arrange
                const params: DestinationAssociationNamesResponseCommandParams = {
                    matrixId: 16,
                    levelId: 0,
                    firstDestinationAssociationId: 0,
                    numberOfDestinationAssociationNamesToFollow: 32,
                    destinationAssociationNameItems: Fixture.buildCharItems(64, 4)
                };
                const options: DestinationAssociationNamesResponseCommandOptions = {
                    lengthOfDestinationAssociatonNamesReturned: NameLength.FOUR_CHAR_NAMES
                };
                let commandIdentifier: CommandIdentifier;
                commandIdentifier = CommandIdentifiers.TX.EXTENDED.DESTINATION_ASSOCIATION_NAMES_RESPONSE_MESSAGE;
                // Act
                const metaCommand = new DestinationAssociationNamesResponseCommand(params, options);
                metaCommand.buildCommand();

                // Assert
                Fixture.assertMetaCommand(metaCommand, commandIdentifier, [
                    {
                        data:
                            'eb 10 00 00 00 00 20 30 30 30 30 30 30 30 31 30 30 30 32 30 30 30 33 30 30 30 34 30 30 30 35 30 30 30 36 30 30 30 37 30 30 30 38 30 30 30 39 30 30 31 30 30 30 31 31 30 30 31 32 30 30 31 33 30 30 31 34 30 30 31 35 30 30 31 36 30 30 31 37 30 30 31 38 30 30 31 39 30 30 32 30 30 30 32 31 30 30 32 32 30 30 32 33 30 30 32 34 30 30 32 35 30 30 32 36 30 30 32 37 30 30 32 38 30 30 32 39 30 30 33 30 30 30 33 31',
                        bytesCount: 0x87,
                        checksum: 0xb2,
                        buffer:
                            '10 02 eb 10 10 00 00 00 00 20 30 30 30 30 30 30 30 31 30 30 30 32 30 30 30 33 30 30 30 34 30 30 30 35 30 30 30 36 30 30 30 37 30 30 30 38 30 30 30 39 30 30 31 30 30 30 31 31 30 30 31 32 30 30 31 33 30 30 31 34 30 30 31 35 30 30 31 36 30 30 31 37 30 30 31 38 30 30 31 39 30 30 32 30 30 30 32 31 30 30 32 32 30 30 32 33 30 30 32 34 30 30 32 35 30 30 32 36 30 30 32 37 30 30 32 38 30 30 32 39 30 30 33 30 30 30 33 31 87 b2 10 03'
                    },
                    {
                        data:
                            'eb 10 00 00 00 20 20 30 30 33 32 30 30 33 33 30 30 33 34 30 30 33 35 30 30 33 36 30 30 33 37 30 30 33 38 30 30 33 39 30 30 34 30 30 30 34 31 30 30 34 32 30 30 34 33 30 30 34 34 30 30 34 35 30 30 34 36 30 30 34 37 30 30 34 38 30 30 34 39 30 30 35 30 30 30 35 31 30 30 35 32 30 30 35 33 30 30 35 34 30 30 35 35 30 30 35 36 30 30 35 37 30 30 35 38 30 30 35 39 30 30 36 30 30 30 36 31 30 30 36 32 30 30 36 33',
                        bytesCount: 0x87,
                        checksum: 0x28,
                        buffer:
                            '10 02 eb 10 10 00 00 00 20 20 30 30 33 32 30 30 33 33 30 30 33 34 30 30 33 35 30 30 33 36 30 30 33 37 30 30 33 38 30 30 33 39 30 30 34 30 30 30 34 31 30 30 34 32 30 30 34 33 30 30 34 34 30 30 34 35 30 30 34 36 30 30 34 37 30 30 34 38 30 30 34 39 30 30 35 30 30 30 35 31 30 30 35 32 30 30 35 33 30 30 35 34 30 30 35 35 30 30 35 36 30 30 35 37 30 30 35 38 30 30 35 39 30 30 36 30 30 30 36 31 30 30 36 32 30 30 36 33 87 28 10 03'
                    }
                ]);
            });

            it('Should create & pack the extended 1 command (...)', () => {
                // Arrange
                const params: DestinationAssociationNamesResponseCommandParams = {
                    matrixId: 0,
                    levelId: 16,
                    firstDestinationAssociationId: 0,
                    numberOfDestinationAssociationNamesToFollow: 32,
                    destinationAssociationNameItems: Fixture.buildCharItems(64, 4)
                };
                const options: DestinationAssociationNamesResponseCommandOptions = {
                    lengthOfDestinationAssociatonNamesReturned: NameLength.FOUR_CHAR_NAMES
                };
                let commandIdentifier: CommandIdentifier;
                commandIdentifier = CommandIdentifiers.TX.EXTENDED.DESTINATION_ASSOCIATION_NAMES_RESPONSE_MESSAGE;
                // Act
                const metaCommand = new DestinationAssociationNamesResponseCommand(params, options);
                metaCommand.buildCommand();

                // Assert
                Fixture.assertMetaCommand(metaCommand, commandIdentifier, [
                    {
                        data:
                            'eb 00 10 00 00 00 20 30 30 30 30 30 30 30 31 30 30 30 32 30 30 30 33 30 30 30 34 30 30 30 35 30 30 30 36 30 30 30 37 30 30 30 38 30 30 30 39 30 30 31 30 30 30 31 31 30 30 31 32 30 30 31 33 30 30 31 34 30 30 31 35 30 30 31 36 30 30 31 37 30 30 31 38 30 30 31 39 30 30 32 30 30 30 32 31 30 30 32 32 30 30 32 33 30 30 32 34 30 30 32 35 30 30 32 36 30 30 32 37 30 30 32 38 30 30 32 39 30 30 33 30 30 30 33 31',
                        bytesCount: 0x87,
                        checksum: 0xb2,
                        buffer:
                            '10 02 eb 00 10 00 00 00 20 30 30 30 30 30 30 30 31 30 30 30 32 30 30 30 33 30 30 30 34 30 30 30 35 30 30 30 36 30 30 30 37 30 30 30 38 30 30 30 39 30 30 31 30 30 30 31 31 30 30 31 32 30 30 31 33 30 30 31 34 30 30 31 35 30 30 31 36 30 30 31 37 30 30 31 38 30 30 31 39 30 30 32 30 30 30 32 31 30 30 32 32 30 30 32 33 30 30 32 34 30 30 32 35 30 30 32 36 30 30 32 37 30 30 32 38 30 30 32 39 30 30 33 30 30 30 33 31 87 b2 10 03'
                    },
                    {
                        data:
                            'eb 00 10 00 00 20 20 30 30 33 32 30 30 33 33 30 30 33 34 30 30 33 35 30 30 33 36 30 30 33 37 30 30 33 38 30 30 33 39 30 30 34 30 30 30 34 31 30 30 34 32 30 30 34 33 30 30 34 34 30 30 34 35 30 30 34 36 30 30 34 37 30 30 34 38 30 30 34 39 30 30 35 30 30 30 35 31 30 30 35 32 30 30 35 33 30 30 35 34 30 30 35 35 30 30 35 36 30 30 35 37 30 30 35 38 30 30 35 39 30 30 36 30 30 30 36 31 30 30 36 32 30 30 36 33',
                        bytesCount: 0x87,
                        checksum: 0x28,
                        buffer:
                            '10 02 eb 00 10 10 00 00 20 20 30 30 33 32 30 30 33 33 30 30 33 34 30 30 33 35 30 30 33 36 30 30 33 37 30 30 33 38 30 30 33 39 30 30 34 30 30 30 34 31 30 30 34 32 30 30 34 33 30 30 34 34 30 30 34 35 30 30 34 36 30 30 34 37 30 30 34 38 30 30 34 39 30 30 35 30 30 30 35 31 30 30 35 32 30 30 35 33 30 30 35 34 30 30 35 35 30 30 35 36 30 30 35 37 30 30 35 38 30 30 35 39 30 30 36 30 30 30 36 31 30 30 36 32 30 30 36 33 87 28 10 03'
                    }
                ]);
            });

        });

        describe('if [DLE] = [0x10] is found it is replaced with [DLE][DLE] to prevent the DATA, BTC or CHK being interpreted as a command', () => {
            // When a DLE is found it is replaced with DLE DLE to prevent the DATA, BTC or CHK being interpreted as a command
            let commandIdentifier: CommandIdentifier;

            beforeAll(() => {
                commandIdentifier = CommandIdentifiers.RX.EXTENDED.ALL_DESTINATIONS_ASSOCIATION_NAMES_REQUEST_MESSAGE;
            });

            it('Should create & pack the general command (...)', () => {
                // Arrange
                const params: DestinationAssociationNamesResponseCommandParams = {
                    matrixId: 16,
                    levelId: 16,
                    firstDestinationAssociationId: 16,
                    numberOfDestinationAssociationNamesToFollow: 16,
                    destinationAssociationNameItems: Fixture.buildCharItems(64, 8)
                };
                const options: DestinationAssociationNamesResponseCommandOptions = {
                    lengthOfDestinationAssociatonNamesReturned: NameLength.EIGHT_CHAR_NAMES
                };
                // Act
                const metaCommand = new DestinationAssociationNamesResponseCommand(params, options);
                metaCommand.buildCommand();

                // Assert
                Fixture.assertMetaCommand(metaCommand, commandIdentifier, [
                    {
                        data:
                            'ea 10 10 01 00 10 10 30 30 30 30 30 30 30 30 30 30 30 30 30 30 30 31 30 30 30 30 30 30 30 32 30 30 30 30 30 30 30 33 30 30 30 30 30 30 30 34 30 30 30 30 30 30 30 35 30 30 30 30 30 30 30 36 30 30 30 30 30 30 30 37 30 30 30 30 30 30 30 38 30 30 30 30 30 30 30 39 30 30 30 30 30 30 31 30 30 30 30 30 30 30 31 31 30 30 30 30 30 30 31 32 30 30 30 30 30 30 31 33 30 30 30 30 30 30 31 34 30 30 30 30 30 30 31 35',
                        bytesCount: 0x87,
                        checksum: 0x0c,
                        buffer:
                            '10 02 ea 10 10 10 10 01 00 10 10 10 10 30 30 30 30 30 30 30 30 30 30 30 30 30 30 30 31 30 30 30 30 30 30 30 32 30 30 30 30 30 30 30 33 30 30 30 30 30 30 30 34 30 30 30 30 30 30 30 35 30 30 30 30 30 30 30 36 30 30 30 30 30 30 30 37 30 30 30 30 30 30 30 38 30 30 30 30 30 30 30 39 30 30 30 30 30 30 31 30 30 30 30 30 30 30 31 31 30 30 30 30 30 30 31 32 30 30 30 30 30 30 31 33 30 30 30 30 30 30 31 34 30 30 30 30 30 30 31 35 87 0c 10 03'
                    },
                    {
                        data:
                            'ea 10 10 01 00 20 10 30 30 30 30 30 30 31 36 30 30 30 30 30 30 31 37 30 30 30 30 30 30 31 38 30 30 30 30 30 30 31 39 30 30 30 30 30 30 32 30 30 30 30 30 30 30 32 31 30 30 30 30 30 30 32 32 30 30 30 30 30 30 32 33 30 30 30 30 30 30 32 34 30 30 30 30 30 30 32 35 30 30 30 30 30 30 32 36 30 30 30 30 30 30 32 37 30 30 30 30 30 30 32 38 30 30 30 30 30 30 32 39 30 30 30 30 30 30 33 30 30 30 30 30 30 30 33 31',
                        bytesCount: 0x87,
                        checksum: 0xd4,
                        buffer:
                            '10 02 ea 10 10 10 10 01 00 20 10 10 30 30 30 30 30 30 31 36 30 30 30 30 30 30 31 37 30 30 30 30 30 30 31 38 30 30 30 30 30 30 31 39 30 30 30 30 30 30 32 30 30 30 30 30 30 30 32 31 30 30 30 30 30 30 32 32 30 30 30 30 30 30 32 33 30 30 30 30 30 30 32 34 30 30 30 30 30 30 32 35 30 30 30 30 30 30 32 36 30 30 30 30 30 30 32 37 30 30 30 30 30 30 32 38 30 30 30 30 30 30 32 39 30 30 30 30 30 30 33 30 30 30 30 30 30 30 33 31 87 d4 10 03'
                    },
                    {
                        data:
                            'ea 10 10 01 00 40 10 30 30 30 30 30 30 33 32 30 30 30 30 30 30 33 33 30 30 30 30 30 30 33 34 30 30 30 30 30 30 33 35 30 30 30 30 30 30 33 36 30 30 30 30 30 30 33 37 30 30 30 30 30 30 33 38 30 30 30 30 30 30 33 39 30 30 30 30 30 30 34 30 30 30 30 30 30 30 34 31 30 30 30 30 30 30 34 32 30 30 30 30 30 30 34 33 30 30 30 30 30 30 34 34 30 30 30 30 30 30 34 35 30 30 30 30 30 30 34 36 30 30 30 30 30 30 34 37',
                        bytesCount: 0x87,
                        checksum: 0x9e,
                        buffer:
                            '10 02 ea 10 10 10 10 01 00 40 10 10 30 30 30 30 30 30 33 32 30 30 30 30 30 30 33 33 30 30 30 30 30 30 33 34 30 30 30 30 30 30 33 35 30 30 30 30 30 30 33 36 30 30 30 30 30 30 33 37 30 30 30 30 30 30 33 38 30 30 30 30 30 30 33 39 30 30 30 30 30 30 34 30 30 30 30 30 30 30 34 31 30 30 30 30 30 30 34 32 30 30 30 30 30 30 34 33 30 30 30 30 30 30 34 34 30 30 30 30 30 30 34 35 30 30 30 30 30 30 34 36 30 30 30 30 30 30 34 37 87 9e 10 03'
                    },
                    {
                        data:
                            'ea 10 10 01 00 70 10 30 30 30 30 30 30 34 38 30 30 30 30 30 30 34 39 30 30 30 30 30 30 35 30 30 30 30 30 30 30 35 31 30 30 30 30 30 30 35 32 30 30 30 30 30 30 35 33 30 30 30 30 30 30 35 34 30 30 30 30 30 30 35 35 30 30 30 30 30 30 35 36 30 30 30 30 30 30 35 37 30 30 30 30 30 30 35 38 30 30 30 30 30 30 35 39 30 30 30 30 30 30 36 30 30 30 30 30 30 30 36 31 30 30 30 30 30 30 36 32 30 30 30 30 30 30 36 33',
                        bytesCount: 0x87,
                        checksum: 0x58,
                        buffer:
                            '10 02 ea 10 10 10 10 01 00 70 10 10 30 30 30 30 30 30 34 38 30 30 30 30 30 30 34 39 30 30 30 30 30 30 35 30 30 30 30 30 30 30 35 31 30 30 30 30 30 30 35 32 30 30 30 30 30 30 35 33 30 30 30 30 30 30 35 34 30 30 30 30 30 30 35 35 30 30 30 30 30 30 35 36 30 30 30 30 30 30 35 37 30 30 30 30 30 30 35 38 30 30 30 30 30 30 35 39 30 30 30 30 30 30 36 30 30 30 30 30 30 30 36 31 30 30 30 30 30 30 36 32 30 30 30 30 30 30 36 33 87 58 10 03'
                    }
                ]);
            });
        });
    });

    describe('to Log Description of the general command', () => {
        it('Should log General command description', () => {
            // Arrange
            const params: DestinationAssociationNamesResponseCommandParams = {
                matrixId: 0,
                levelId: 0,
                firstDestinationAssociationId: 0,
                numberOfDestinationAssociationNamesToFollow: 32,
                destinationAssociationNameItems: Fixture.buildCharItems(64, 8)
            };
            const options: DestinationAssociationNamesResponseCommandOptions = {
                lengthOfDestinationAssociatonNamesReturned: NameLength.EIGHT_CHAR_NAMES
            };
            // Act
            const metaCommand = new DestinationAssociationNamesResponseCommand(params, options);
            const description = metaCommand.toLogDescription();
            console.log(description);

            // Assert
            expect(description.toLowerCase().startsWith('general')).toBe(true);
        });
    });
});
